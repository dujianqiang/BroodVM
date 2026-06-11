package service_test

import (
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
