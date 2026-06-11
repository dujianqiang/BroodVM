package service_test

import (
	"strings"
	"testing"
	"time"

	"github.com/dujianqiang/broodvm/internal/config"
	lv "github.com/dujianqiang/broodvm/internal/libvirt"
	"github.com/dujianqiang/broodvm/internal/service"
	"github.com/dujianqiang/broodvm/internal/store"
)

func newTestDeps(t *testing.T) (*store.VMStore, *store.TaskStore, *lv.MockClient, *config.Config) {
	t.Helper()
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	cfg := &config.Config{}
	cfg.Host.Bridge = "br0"
	cfg.Host.ImageDir = t.TempDir()
	cfg.Host.SSHKey = "/root/.ssh/id_ed25519"
	return store.NewVMStore(db), store.NewTaskStore(db), lv.NewMock(), cfg
}

func TestVMService_Start(t *testing.T) {
	vs, ts, mock, cfg := newTestDeps(t)
	svc := service.NewVMService(cfg, vs, ts, mock, "/tmp/seed.img")

	vm := &store.VM{
		ID: "vm-id-1", Name: "vm-test",
		VCPU: 1, MemoryGB: 1, DiskGB: 10,
		NetworkType: "nat", MAC: "52:54:00:aa:bb:01",
		VNCPort: 5900, Status: "stopped", CreatedAt: time.Now().UTC(),
	}
	vs.Create(vm)

	taskID, err := svc.Start("vm-id-1")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if taskID == "" {
		t.Fatal("taskID should not be empty")
	}
	task, err := ts.Get(taskID)
	if err != nil {
		t.Fatalf("Get task: %v", err)
	}
	if task.Type != "start_vm" {
		t.Errorf("task type = %q, want start_vm", task.Type)
	}

	// 等待后台 goroutine 完成
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
		task, _ = ts.Get(taskID)
		if task.Status == "success" || task.Status == "failed" {
			break
		}
	}
	if task.Status != "success" {
		t.Errorf("task status = %q, want success", task.Status)
	}
	got, _ := vs.Get("vm-id-1")
	if got.Status != "running" {
		t.Errorf("vm status = %q, want running", got.Status)
	}
}

func TestVMService_Stop(t *testing.T) {
	vs, ts, mock, cfg := newTestDeps(t)
	svc := service.NewVMService(cfg, vs, ts, mock, "/tmp/seed.img")

	mock.RunningDomains["vm-test"] = true
	vm := &store.VM{
		ID: "vm-id-2", Name: "vm-test",
		VCPU: 1, MemoryGB: 1, DiskGB: 10,
		NetworkType: "nat", MAC: "52:54:00:aa:bb:02",
		VNCPort: 5901, Status: "running", IP: "10.0.0.5", CreatedAt: time.Now().UTC(),
	}
	vs.Create(vm)

	taskID, err := svc.Stop("vm-id-2")
	if err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if taskID == "" {
		t.Fatal("taskID should not be empty")
	}

	// 等待后台 goroutine 完成
	deadline := time.Now().Add(5 * time.Second)
	var task *store.Task
	for time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
		task, _ = ts.Get(taskID)
		if task.Status == "success" || task.Status == "failed" {
			break
		}
	}
	if task.Status != "success" {
		t.Errorf("task status = %q, want success", task.Status)
	}
	got, _ := vs.Get("vm-id-2")
	if got.Status != "stopped" {
		t.Errorf("vm status = %q, want stopped", got.Status)
	}
}

func TestVMService_CreateRecordsTask(t *testing.T) {
	vs, ts, mock, cfg := newTestDeps(t)
	svc := service.NewVMService(cfg, vs, ts, mock, "/tmp/seed.img")

	taskID, err := svc.Create(service.CreateVMReq{
		Name: "vm-new", VCPU: 2, MemoryGB: 4, DiskGB: 20, NetworkType: "nat",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if taskID == "" {
		t.Fatal("taskID should not be empty")
	}

	vms, _ := vs.List()
	if len(vms) != 1 || vms[0].Name != "vm-new" || vms[0].Status != "creating" {
		t.Errorf("unexpected vms: %+v", vms)
	}

	task, err := ts.Get(taskID)
	if err != nil {
		t.Fatalf("Get task: %v", err)
	}
	if task.Type != "create_vm" {
		t.Errorf("task type = %q, want create_vm", task.Type)
	}
}

func TestCreate_BridgeAutoAssignsPoolIP(t *testing.T) {
	vs, ts, mock, cfg := newTestDeps(t)
	cfg.Host.IPPool.Gateway = "192.168.1.1"
	cfg.Host.IPPool.DNS = "8.8.8.8"
	cfg.Host.IPPool.IPs = []string{"192.168.1.100/24", "192.168.1.101/24"}
	svc := service.NewVMService(cfg, vs, ts, mock, "/tmp/seed.img")

	_, err := svc.Create(service.CreateVMReq{
		Name: "vm-b1", VCPU: 1, MemoryGB: 1, DiskGB: 10, NetworkType: "bridge",
	})
	if err != nil {
		t.Fatalf("Create vm-b1: %v", err)
	}
	_, err = svc.Create(service.CreateVMReq{
		Name: "vm-b2", VCPU: 1, MemoryGB: 1, DiskGB: 10, NetworkType: "bridge",
	})
	if err != nil {
		t.Fatalf("Create vm-b2: %v", err)
	}

	vms, _ := vs.List()
	if len(vms) != 2 {
		t.Fatalf("expected 2 vms, got %d", len(vms))
	}
	seen := make(map[string]bool)
	for _, vm := range vms {
		if vm.IP == "" {
			t.Errorf("vm %s has no IP", vm.Name)
		}
		if seen[vm.IP] {
			t.Errorf("duplicate IP %s", vm.IP)
		}
		seen[vm.IP] = true
	}
	if !seen["192.168.1.100/24"] || !seen["192.168.1.101/24"] {
		t.Errorf("expected both pool IPs assigned, got %v", seen)
	}
}

func TestCreate_BridgePoolExhausted(t *testing.T) {
	vs, ts, mock, cfg := newTestDeps(t)
	cfg.Host.IPPool.Gateway = "192.168.1.1"
	cfg.Host.IPPool.IPs = []string{"192.168.1.100/24"}
	svc := service.NewVMService(cfg, vs, ts, mock, "/tmp/seed.img")

	_, err := svc.Create(service.CreateVMReq{
		Name: "vm-b1", VCPU: 1, MemoryGB: 1, DiskGB: 10, NetworkType: "bridge",
	})
	if err != nil {
		t.Fatalf("Create vm-b1: %v", err)
	}
	_, err = svc.Create(service.CreateVMReq{
		Name: "vm-b2", VCPU: 1, MemoryGB: 1, DiskGB: 10, NetworkType: "bridge",
	})
	if err == nil {
		t.Fatal("expected error when pool exhausted, got nil")
	}
}

func TestCreate_BridgeIPReuseAfterDelete(t *testing.T) {
	vs, ts, mock, cfg := newTestDeps(t)
	cfg.Host.IPPool.Gateway = "192.168.1.1"
	cfg.Host.IPPool.IPs = []string{"192.168.1.100/24"}
	svc := service.NewVMService(cfg, vs, ts, mock, "/tmp/seed.img")

	_, err := svc.Create(service.CreateVMReq{
		Name: "vm-b1", VCPU: 1, MemoryGB: 1, DiskGB: 10, NetworkType: "bridge",
	})
	if err != nil {
		t.Fatalf("Create vm-b1: %v", err)
	}

	// 模拟删除：将 vm-b1 的 status 置为 deleted
	vms, _ := vs.List()
	vs.UpdateStatus(vms[0].ID, "deleted", "")

	// 现在池里的 IP 应该可以被复用
	_, err = svc.Create(service.CreateVMReq{
		Name: "vm-b2", VCPU: 1, MemoryGB: 1, DiskGB: 10, NetworkType: "bridge",
	})
	if err != nil {
		t.Fatalf("Create vm-b2 after delete: %v", err)
	}
	// vm-b1 已 deleted，List() 不返回它
	vms2, _ := vs.List()
	if len(vms2) != 1 || vms2[0].Name != "vm-b2" {
		t.Fatalf("expected 1 vm (vm-b2), got %v", vms2)
	}
	if vms2[0].IP != "192.168.1.100/24" {
		t.Errorf("vm-b2 ip = %q, want 192.168.1.100/24", vms2[0].IP)
	}
}

func TestCreate_BridgeIPReuseAfterError(t *testing.T) {
	vs, ts, mock, cfg := newTestDeps(t)
	cfg.Host.IPPool.Gateway = "192.168.1.1"
	cfg.Host.IPPool.IPs = []string{"192.168.1.100/24"}
	svc := service.NewVMService(cfg, vs, ts, mock, "/tmp/seed.img")

	_, err := svc.Create(service.CreateVMReq{
		Name: "vm-b1", VCPU: 1, MemoryGB: 1, DiskGB: 10, NetworkType: "bridge",
	})
	if err != nil {
		t.Fatalf("Create vm-b1: %v", err)
	}

	// 模拟创建失败：将 vm-b1 的 status 置为 error
	vms, _ := vs.List()
	vs.UpdateStatus(vms[0].ID, "error", "")

	// error 状态 VM 的 IP 应该可以被复用
	_, err = svc.Create(service.CreateVMReq{
		Name: "vm-b2", VCPU: 1, MemoryGB: 1, DiskGB: 10, NetworkType: "bridge",
	})
	if err != nil {
		t.Fatalf("Create vm-b2 after error: %v", err)
	}
	// vm-b1 status=error，List() 仍返回它（不过滤 error）
	vms2, _ := vs.List()
	var vm2 *store.VM
	for _, v := range vms2 {
		if v.Name == "vm-b2" {
			vm2 = v
		}
	}
	if vm2 == nil {
		t.Fatal("vm-b2 not found")
	}
	if vm2.IP != "192.168.1.100/24" {
		t.Errorf("vm-b2 ip = %q, want 192.168.1.100/24", vm2.IP)
	}
}

func TestCreate_BridgeAutoDetect_InvalidInterface(t *testing.T) {
	vs, ts, mock, cfg := newTestDeps(t)
	cfg.Host.Bridge = "nonexistent-br-xyz"
	// ip_pool.ips 为空 → 触发自动检测
	cfg.Host.IPPool.Gateway = "192.168.56.2"
	svc := service.NewVMService(cfg, vs, ts, mock, "/tmp/seed.img")

	_, err := svc.Create(service.CreateVMReq{
		Name: "vm-auto", VCPU: 1, MemoryGB: 1, DiskGB: 10, NetworkType: "bridge",
	})
	if err == nil {
		t.Fatal("expected error for nonexistent bridge interface")
	}
	if !strings.Contains(err.Error(), "无法读取网桥") {
		t.Errorf("unexpected error message: %v", err)
	}
}
