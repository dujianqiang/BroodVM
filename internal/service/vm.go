package service

import (
	"bytes"
	"crypto/rand"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"text/template"
	"time"

	"github.com/dujianqiang/broodvm/internal/cloudinit"
	"github.com/dujianqiang/broodvm/internal/config"
	lv "github.com/dujianqiang/broodvm/internal/libvirt"
	"github.com/dujianqiang/broodvm/internal/store"
	"github.com/google/uuid"
)

type VMService struct {
	cfg       *config.Config
	vmStore   *store.VMStore
	taskStore *store.TaskStore
	virt      lv.Client
	seedImage string
	tmpl      *template.Template
}

func NewVMService(
	cfg *config.Config,
	vmStore *store.VMStore,
	taskStore *store.TaskStore,
	virtClient lv.Client,
	seedImage string,
) *VMService {
	return &VMService{
		cfg:       cfg,
		vmStore:   vmStore,
		taskStore: taskStore,
		virt:      virtClient,
		seedImage: seedImage,
	}
}

// SetTemplate 注入已解析的 XML 模板（由 main.go 通过 embed.FS 传入）。
func (s *VMService) SetTemplate(tmpl *template.Template) {
	s.tmpl = tmpl
}

type CreateVMReq struct {
	Name        string `json:"Name"`
	VCPU        int    `json:"VCPU"`
	MemoryGB    int    `json:"MemoryGB"`
	DiskGB      int    `json:"DiskGB"`
	NetworkType string `json:"NetworkType"`
}

// Create 创建 VM 记录和 task 记录，启动异步 goroutine，返回 task_id。
func (s *VMService) Create(req CreateVMReq) (string, error) {
	vncPort, err := s.vmStore.NextVNCPort()
	if err != nil {
		return "", fmt.Errorf("alloc vnc port: %w", err)
	}

	vmID := uuid.NewString()
	taskID := uuid.NewString()
	now := time.Now().UTC()

	vm := &store.VM{
		ID:          vmID,
		Name:        req.Name,
		VCPU:        req.VCPU,
		MemoryGB:    req.MemoryGB,
		DiskGB:      req.DiskGB,
		NetworkType: req.NetworkType,
		MAC:         generateMAC(),
		VNCPort:     vncPort,
		Status:      "creating",
		CreatedAt:   now,
	}
	if err := s.vmStore.Create(vm); err != nil {
		return "", fmt.Errorf("create vm record: %w", err)
	}

	task := &store.Task{
		ID: taskID, Type: "create_vm", RefID: vmID,
		Status: "running", Progress: 0, Message: "正在准备...",
		CreatedAt: now,
	}
	if err := s.taskStore.Create(task); err != nil {
		return "", err
	}

	go s.runCreate(vm, taskID)
	return taskID, nil
}

func (s *VMService) runCreate(vm *store.VM, taskID string) {
	fail := func(msg string, err error) {
		s.taskStore.Update(taskID, "failed", 0, fmt.Sprintf("%s: %v", msg, err))
		s.vmStore.UpdateStatus(vm.ID, "error", "")
	}
	progress := func(pct int, msg string) {
		s.taskStore.Update(taskID, "running", pct, msg)
	}

	imageDir := s.cfg.Host.ImageDir

	// 1. 创建 qcow2 差量镜像
	progress(10, "正在创建磁盘镜像...")
	diskPath := filepath.Join(imageDir, vm.Name+".qcow2")
	out, err := exec.Command("qemu-img", "create",
		"-f", "qcow2",
		"-b", s.seedImage,
		"-F", "qcow2",
		diskPath,
		fmt.Sprintf("%dG", vm.DiskGB),
	).CombinedOutput()
	if err != nil {
		fail("创建磁盘失败", fmt.Errorf("%w\n%s", err, out))
		return
	}

	// 2. 生成 cloud-init ISO
	progress(30, "正在生成 cloud-init 配置...")
	sshPubKey, err := os.ReadFile(s.cfg.Host.SSHKey + ".pub")
	if err != nil {
		fail("读取 SSH 公钥失败", err)
		return
	}
	cidataPath, err := cloudinit.Generate(imageDir, vm.Name, string(sshPubKey))
	if err != nil {
		fail("生成 cidata 失败", err)
		return
	}

	// 3. 渲染 XML + define + start
	progress(50, "正在定义虚拟机...")
	xmlDesc, err := s.renderXML(vm, diskPath, cidataPath)
	if err != nil {
		fail("渲染 XML 失败", err)
		return
	}
	if err := s.virt.DefineAndStart(xmlDesc); err != nil {
		fail("启动虚拟机失败", err)
		return
	}

	// 4. 轮询等待 VM 就绪（最长 3 分钟）
	progress(70, "正在等待虚拟机启动...")
	deadline := time.Now().Add(3 * time.Minute)
	var vmIP string
	for time.Now().Before(deadline) {
		time.Sleep(5 * time.Second)
		running, _ := s.virt.IsRunning(vm.Name)
		if running {
			vmIP, _ = s.virt.GetIP(vm.Name)
			if vmIP != "" {
				break
			}
		}
	}

	s.vmStore.UpdateStatus(vm.ID, "running", vmIP)
	s.taskStore.Update(taskID, "success", 100, "虚拟机已就绪")
}

// Delete 创建删除 task 并启动异步 goroutine，返回 task_id。
func (s *VMService) Delete(vmID string) (string, error) {
	vm, err := s.vmStore.Get(vmID)
	if err != nil {
		return "", err
	}

	taskID := uuid.NewString()
	task := &store.Task{
		ID: taskID, Type: "delete_vm", RefID: vmID,
		Status: "running", Progress: 0, Message: "正在删除...",
		CreatedAt: time.Now().UTC(),
	}
	if err := s.taskStore.Create(task); err != nil {
		return "", err
	}

	go s.runDelete(vm, taskID)
	return taskID, nil
}

func (s *VMService) runDelete(vm *store.VM, taskID string) {
	fail := func(msg string, err error) {
		s.taskStore.Update(taskID, "failed", 0, fmt.Sprintf("%s: %v", msg, err))
	}

	// 1. 强制关机（若在运行）
	s.taskStore.Update(taskID, "running", 20, "正在强制关闭虚拟机...")
	if running, _ := s.virt.IsRunning(vm.Name); running {
		if err := s.virt.Destroy(vm.Name); err != nil {
			fail("强制关闭失败", err)
			return
		}
	}

	// 2. 删除定义
	s.taskStore.Update(taskID, "running", 50, "正在注销虚拟机定义...")
	if err := s.virt.Undefine(vm.Name); err != nil {
		fail("注销定义失败", err)
		return
	}

	// 3. 删除磁盘文件
	s.taskStore.Update(taskID, "running", 70, "正在删除磁盘文件...")
	imageDir := s.cfg.Host.ImageDir
	os.Remove(filepath.Join(imageDir, vm.Name+".qcow2"))
	os.Remove(filepath.Join(imageDir, vm.Name+"-cidata.iso"))

	s.vmStore.Delete(vm.ID)
	s.taskStore.Update(taskID, "success", 100, "虚拟机已删除")
}

func (s *VMService) Start(vmID string) error {
	vm, err := s.vmStore.Get(vmID)
	if err != nil {
		return err
	}
	if err := s.virt.Start(vm.Name); err != nil {
		return err
	}
	return s.vmStore.UpdateStatus(vmID, "running", vm.IP)
}

func (s *VMService) Stop(vmID string) error {
	vm, err := s.vmStore.Get(vmID)
	if err != nil {
		return err
	}
	if err := s.virt.Shutdown(vm.Name); err != nil {
		return err
	}
	return s.vmStore.UpdateStatus(vmID, "stopped", vm.IP)
}

func (s *VMService) Restart(vmID string) error {
	vm, err := s.vmStore.Get(vmID)
	if err != nil {
		return err
	}
	if err := s.virt.Reboot(vm.Name); err != nil {
		return err
	}
	return s.vmStore.UpdateStatus(vmID, "running", vm.IP)
}

type domainData struct {
	Name, UUID, MAC, Bridge, DiskPath, CidataPath, NetworkType string
	MemoryGB, VCPU, VNCPort                                     int
}

func (s *VMService) renderXML(vm *store.VM, diskPath, cidataPath string) (string, error) {
	data := domainData{
		Name:        vm.Name,
		UUID:        vm.ID,
		MAC:         vm.MAC,
		Bridge:      s.cfg.Host.Bridge,
		DiskPath:    diskPath,
		CidataPath:  cidataPath,
		NetworkType: vm.NetworkType,
		MemoryGB:    vm.MemoryGB,
		VCPU:        vm.VCPU,
		VNCPort:     vm.VNCPort,
	}
	var buf bytes.Buffer
	if err := s.tmpl.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func generateMAC() string {
	b := make([]byte, 3)
	rand.Read(b) //nolint:errcheck
	return fmt.Sprintf("52:54:00:%02x:%02x:%02x", b[0], b[1], b[2])
}

func (s *VMService) ListVMs() ([]*store.VM, error) { return s.vmStore.List() }
func (s *VMService) GetVM(id string) (*store.VM, error) { return s.vmStore.Get(id) }
func (s *VMService) GetTask(id string) (*store.Task, error) { return s.taskStore.Get(id) }
