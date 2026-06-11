package service

import (
	"bytes"
	"crypto/rand"
	"fmt"
	"log"
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
	// 静态 IP 配置（可选，留空则 DHCP）
	IP      string `json:"IP"`      // CIDR，如 192.168.1.100/24
	Gateway string `json:"Gateway"` // 如 192.168.1.1
	DNS     string `json:"DNS"`     // 逗号分隔，留空默认 8.8.8.8
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
		IP:          req.IP,
		Gateway:     req.Gateway,
		DNS:         req.DNS,
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
	var netCfg *cloudinit.NetworkConfig
	if vm.IP != "" {
		netCfg = &cloudinit.NetworkConfig{
			IP:      vm.IP,
			Gateway: vm.Gateway,
			DNS:     vm.DNS,
			MAC:     vm.MAC,
		}
	}
	cidataPath, err := cloudinit.Generate(imageDir, vm.Name, string(sshPubKey), netCfg)
	if err != nil {
		fail("生成 cidata 失败", err)
		return
	}

	// 3. 渲染 XML + 保存到 xml/ 子目录 + define + start
	progress(50, "正在定义虚拟机...")
	xmlDesc, err := s.renderXML(vm, diskPath, cidataPath)
	if err != nil {
		fail("渲染 XML 失败", err)
		return
	}
	xmlDir := filepath.Join(imageDir, "xml")
	if err := os.MkdirAll(xmlDir, 0755); err == nil {
		os.WriteFile(filepath.Join(xmlDir, vm.Name+".xml"), []byte(xmlDesc), 0644) //nolint:errcheck
	}
	if err := s.virt.DefineAndStart(xmlDesc); err != nil {
		fail("启动虚拟机失败", err)
		return
	}

	// 4. 轮询等待 VM 就绪（最长 3 分钟）
	progress(70, "正在等待虚拟机启动...")
	deadline := time.Now().Add(3 * time.Minute)
	finalIP := vm.IP // 静态 IP 已知，直接用；DHCP 则等待发现
	if vm.IP == "" {
		// DHCP 模式：轮询直到拿到 IP
		for time.Now().Before(deadline) {
			time.Sleep(5 * time.Second)
			if running, _ := s.virt.IsRunning(vm.Name); running {
				if ip, _ := s.virt.GetIP(vm.Name, vm.MAC); ip != "" {
					finalIP = ip
					break
				}
			}
		}
	} else {
		// 静态 IP 模式：只等待 VM 进入 running 状态
		for time.Now().Before(deadline) {
			time.Sleep(5 * time.Second)
			if running, _ := s.virt.IsRunning(vm.Name); running {
				break
			}
		}
	}

	s.vmStore.UpdateStatus(vm.ID, "running", finalIP)
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

	// 3. 删除磁盘文件及 XML
	s.taskStore.Update(taskID, "running", 70, "正在删除磁盘文件...")
	imageDir := s.cfg.Host.ImageDir
	os.Remove(filepath.Join(imageDir, vm.Name+".qcow2"))
	os.Remove(filepath.Join(imageDir, vm.Name+"-cidata.iso"))
	os.Remove(filepath.Join(imageDir, "xml", vm.Name+".xml"))

	s.vmStore.Delete(vm.ID)
	s.taskStore.Update(taskID, "success", 100, "虚拟机已删除")
}

func (s *VMService) Start(vmID string) (string, error) {
	vm, err := s.vmStore.Get(vmID)
	if err != nil {
		return "", err
	}
	taskID := uuid.NewString()
	task := &store.Task{
		ID: taskID, Type: "start_vm", RefID: vmID,
		Status: "running", Progress: 0, Message: "正在启动...",
		CreatedAt: time.Now().UTC(),
	}
	if err := s.taskStore.Create(task); err != nil {
		return "", err
	}
	go s.runStart(vm, taskID)
	return taskID, nil
}

func (s *VMService) runStart(vm *store.VM, taskID string) {
	fail := func(msg string) {
		s.taskStore.Update(taskID, "failed", 0, msg)
		s.vmStore.UpdateStatus(vm.ID, "stopped", vm.IP)
	}

	s.taskStore.Update(taskID, "running", 20, "正在发送启动指令...")
	if err := s.virt.Start(vm.Name); err != nil {
		fail("启动失败: " + err.Error())
		return
	}

	s.taskStore.Update(taskID, "running", 50, "等待虚拟机就绪...")
	deadline := time.Now().Add(2 * time.Minute)
	for time.Now().Before(deadline) {
		time.Sleep(3 * time.Second)
		if running, _ := s.virt.IsRunning(vm.Name); running {
			ip, _ := s.virt.GetIP(vm.Name, vm.MAC)
			s.vmStore.UpdateStatus(vm.ID, "running", ip)
			s.taskStore.Update(taskID, "success", 100, "虚拟机已启动")
			return
		}
	}
	fail("启动超时")
}

func (s *VMService) Stop(vmID string) (string, error) {
	vm, err := s.vmStore.Get(vmID)
	if err != nil {
		return "", err
	}
	taskID := uuid.NewString()
	task := &store.Task{
		ID: taskID, Type: "stop_vm", RefID: vmID,
		Status: "running", Progress: 0, Message: "正在关机...",
		CreatedAt: time.Now().UTC(),
	}
	if err := s.taskStore.Create(task); err != nil {
		return "", err
	}
	go s.runStop(vm, taskID)
	return taskID, nil
}

func (s *VMService) runStop(vm *store.VM, taskID string) {
	s.taskStore.Update(taskID, "running", 20, "正在发送关机指令...")
	if err := s.virt.Shutdown(vm.Name); err != nil {
		s.taskStore.Update(taskID, "failed", 0, "关机失败: "+err.Error())
		return
	}

	s.taskStore.Update(taskID, "running", 50, "等待虚拟机关机...")
	deadline := time.Now().Add(2 * time.Minute)
	for time.Now().Before(deadline) {
		time.Sleep(3 * time.Second)
		if running, _ := s.virt.IsRunning(vm.Name); !running {
			s.vmStore.UpdateStatus(vm.ID, "stopped", vm.IP)
			s.taskStore.Update(taskID, "success", 100, "虚拟机已关机")
			return
		}
	}
	s.taskStore.Update(taskID, "failed", 0, "关机超时")
}

func (s *VMService) Restart(vmID string) (string, error) {
	vm, err := s.vmStore.Get(vmID)
	if err != nil {
		return "", err
	}
	taskID := uuid.NewString()
	task := &store.Task{
		ID: taskID, Type: "restart_vm", RefID: vmID,
		Status: "running", Progress: 0, Message: "正在重启...",
		CreatedAt: time.Now().UTC(),
	}
	if err := s.taskStore.Create(task); err != nil {
		return "", err
	}
	go s.runRestart(vm, taskID)
	return taskID, nil
}

func (s *VMService) runRestart(vm *store.VM, taskID string) {
	s.taskStore.Update(taskID, "running", 20, "正在发送重启指令...")
	if err := s.virt.Reboot(vm.Name); err != nil {
		s.taskStore.Update(taskID, "failed", 0, "重启失败: "+err.Error())
		return
	}

	// 等 VM 短暂下线后再检测恢复
	time.Sleep(5 * time.Second)
	s.taskStore.Update(taskID, "running", 60, "等待虚拟机恢复...")
	deadline := time.Now().Add(2 * time.Minute)
	for time.Now().Before(deadline) {
		time.Sleep(3 * time.Second)
		if running, _ := s.virt.IsRunning(vm.Name); running {
			s.taskStore.Update(taskID, "success", 100, "虚拟机已重启")
			return
		}
	}
	s.taskStore.Update(taskID, "failed", 0, "重启超时")
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

// SyncRunningIPs 在服务启动时调用，对 running 但 IP 为空的 VM 补全 IP。
func (s *VMService) SyncRunningIPs() {
	vms, err := s.vmStore.List()
	if err != nil {
		return
	}
	for _, vm := range vms {
		if vm.Status == "running" && vm.IP == "" {
			if ip, _ := s.virt.GetIP(vm.Name, vm.MAC); ip != "" {
				s.vmStore.UpdateStatus(vm.ID, vm.Status, ip)
				log.Printf("synced IP for %s: %s", vm.Name, ip)
			}
		}
	}
}
func (s *VMService) GetTask(id string) (*store.Task, error) { return s.taskStore.Get(id) }
