package handler_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/dujianqiang/broodvm/internal/api/handler"
	"github.com/dujianqiang/broodvm/internal/config"
	lv "github.com/dujianqiang/broodvm/internal/libvirt"
	"github.com/dujianqiang/broodvm/internal/service"
	"github.com/dujianqiang/broodvm/internal/store"
)

func newVMTestEnv(t *testing.T) (*gin.Engine, *store.VMStore, *store.TaskStore) {
	t.Helper()
	db, _ := store.Open(":memory:")
	t.Cleanup(func() { db.Close() })

	cfg := &config.Config{}
	cfg.Host.ImageDir = t.TempDir()
	cfg.Host.SSHKey = "/root/.ssh/id_ed25519"

	vs := store.NewVMStore(db)
	ts := store.NewTaskStore(db)
	mock := lv.NewMock()
	svc := service.NewVMService(cfg, vs, ts, mock, "/tmp/seed.img")

	vmH := handler.NewVMHandler(svc)
	taskH := handler.NewTaskHandler(ts)

	r := gin.New()
	r.GET("/api/vms", vmH.List)
	r.POST("/api/vms", vmH.Create)
	r.GET("/api/vms/:id", vmH.Get)
	r.DELETE("/api/vms/:id", vmH.Delete)
	r.POST("/api/vms/:id/start", vmH.Start)
	r.POST("/api/vms/:id/stop", vmH.Stop)
	r.POST("/api/vms/:id/restart", vmH.Restart)
	r.GET("/api/tasks/:task_id", taskH.Get)

	return r, vs, ts
}

func TestVMHandler_List(t *testing.T) {
	r, vs, _ := newVMTestEnv(t)
	vs.Create(&store.VM{
		ID: "id1", Name: "vm-001",
		VCPU: 1, MemoryGB: 1, DiskGB: 10,
		NetworkType: "nat", MAC: "52:54:00:00:00:01",
		VNCPort: 5900, Status: "running", CreatedAt: time.Now().UTC(),
	})

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/vms", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	var resp []map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	if len(resp) != 1 {
		t.Errorf("len = %d, want 1", len(resp))
	}
}

func TestVMHandler_Create(t *testing.T) {
	r, vs, _ := newVMTestEnv(t)
	body, _ := json.Marshal(map[string]any{
		"Name": "vm-new", "VCPU": 2, "MemoryGB": 4, "DiskGB": 20, "NetworkType": "nat",
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/vms", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", w.Code)
	}
	var resp map[string]string
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["task_id"] == "" {
		t.Error("task_id should not be empty")
	}
	vms, _ := vs.List()
	if len(vms) != 1 {
		t.Errorf("vm count = %d, want 1", len(vms))
	}
}
