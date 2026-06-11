package store_test

import (
	"testing"
	"time"

	"github.com/dujianqiang/broodvm/internal/store"
	"github.com/google/uuid"
)

func openTestDB(t *testing.T) *store.DB {
	t.Helper()
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestVMStore_CreateAndGet(t *testing.T) {
	db := openTestDB(t)
	vs := store.NewVMStore(db)

	vm := &store.VM{
		ID:          uuid.NewString(),
		Name:        "vm-001",
		VCPU:        2,
		MemoryGB:    4,
		DiskGB:      20,
		NetworkType: "bridge",
		MAC:         "52:54:00:aa:bb:cc",
		VNCPort:     5900,
		Status:      "creating",
		CreatedAt:   time.Now().UTC(),
	}
	if err := vs.Create(vm); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := vs.Get(vm.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Name != "vm-001" {
		t.Errorf("Name = %q, want vm-001", got.Name)
	}
}

func TestVMStore_List(t *testing.T) {
	db := openTestDB(t)
	vs := store.NewVMStore(db)

	for i, name := range []string{"vm-a", "vm-b"} {
		vs.Create(&store.VM{
			ID: uuid.NewString(), Name: name,
			VCPU: 1, MemoryGB: 1, DiskGB: 10,
			NetworkType: "nat", MAC: "52:54:00:00:00:0" + string(rune('0'+i)),
			VNCPort: 5900 + i, Status: "running", CreatedAt: time.Now().UTC(),
		})
	}

	vms, err := vs.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(vms) != 2 {
		t.Errorf("len = %d, want 2", len(vms))
	}
}

func TestVMStore_NextVNCPort(t *testing.T) {
	db := openTestDB(t)
	vs := store.NewVMStore(db)

	port, err := vs.NextVNCPort()
	if err != nil {
		t.Fatalf("NextVNCPort: %v", err)
	}
	if port != 5900 {
		t.Errorf("port = %d, want 5900", port)
	}

	vs.Create(&store.VM{
		ID: uuid.NewString(), Name: "vm-x",
		VCPU: 1, MemoryGB: 1, DiskGB: 10,
		NetworkType: "nat", MAC: "52:54:00:00:00:01",
		VNCPort: 5900, Status: "running", CreatedAt: time.Now().UTC(),
	})

	port2, _ := vs.NextVNCPort()
	if port2 != 5901 {
		t.Errorf("port2 = %d, want 5901", port2)
	}
}

func TestVMStore_UpdateStatus(t *testing.T) {
	db := openTestDB(t)
	vs := store.NewVMStore(db)

	id := uuid.NewString()
	vs.Create(&store.VM{
		ID: id, Name: "vm-y",
		VCPU: 1, MemoryGB: 1, DiskGB: 10,
		NetworkType: "nat", MAC: "52:54:00:00:00:02",
		VNCPort: 5902, Status: "creating", CreatedAt: time.Now().UTC(),
	})

	vs.UpdateStatus(id, "running", "192.168.1.10")
	got, _ := vs.Get(id)
	if got.Status != "running" || got.IP != "192.168.1.10" {
		t.Errorf("got status=%q ip=%q", got.Status, got.IP)
	}
}

func TestTaskStore_CreateUpdateGet(t *testing.T) {
	db := openTestDB(t)
	ts := store.NewTaskStore(db)

	task := &store.Task{
		ID:        uuid.NewString(),
		Type:      "create_vm",
		RefID:     uuid.NewString(),
		Status:    "running",
		Progress:  0,
		Message:   "正在准备...",
		CreatedAt: time.Now().UTC(),
	}
	if err := ts.Create(task); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := ts.Update(task.ID, "success", 100, "完成"); err != nil {
		t.Fatalf("Update: %v", err)
	}

	got, err := ts.Get(task.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Status != "success" || got.Progress != 100 {
		t.Errorf("got status=%q progress=%d", got.Status, got.Progress)
	}
}
