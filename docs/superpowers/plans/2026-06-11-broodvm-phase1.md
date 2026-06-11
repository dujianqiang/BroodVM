# BroodVM Phase 1 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 BroodVM Phase 1 — 基于 KVM/QEMU 的单宿主机 VM 管理后台，提供 Web 界面支持 VM 创建、启停、删除全生命周期管理。

**Architecture:** Gin HTTP 服务接收请求，libvirt Go binding 操作 KVM 虚拟机，SQLite 存储 VM 元数据和任务状态；VM 创建/删除通过 goroutine 异步执行并通过 task API 轮询进度；前端静态文件通过 embed.FS 打包进二进制。

**Tech Stack:** Go 1.22, gin-gonic/gin v1.9, libvirt.org/go/libvirt v1.10005, modernc.org/sqlite v1.30, gopkg.in/yaml.v3 v3, google/uuid v1.6, jQuery 3.7 + Pico.css 2.0 (CDN)

**宿主机前置依赖（Ubuntu 22.04）：**
```bash
apt install -y libvirt-dev genisoimage qemu-utils qemu-kvm
```

---

## 文件结构总览

```
broodvm/
├── go.mod
├── go.sum
├── main.go                          # 程序入口，依赖组装 + embed
├── config.yaml                      # 默认配置
├── internal/
│   ├── config/
│   │   ├── config.go               # 配置结构 + 加载 + 种子镜像逻辑
│   │   └── config_test.go
│   ├── store/
│   │   ├── db.go                   # SQLite 初始化 + 建表
│   │   ├── vm.go                   # VM CRUD
│   │   ├── task.go                 # Task CRUD
│   │   └── store_test.go
│   ├── libvirt/
│   │   ├── client.go               # Client 接口 + 真实 libvirt 实现
│   │   └── mock.go                 # 测试用 MockClient
│   ├── cloudinit/
│   │   └── gen.go                  # 生成 cloud-init cidata ISO
│   ├── service/
│   │   ├── session.go              # 内存 session 存储
│   │   ├── vm.go                   # VMService：创建/删除/启停（含 goroutine）
│   │   └── vm_test.go
│   └── api/
│       ├── router.go               # Gin 路由注册
│       ├── middleware/
│       │   └── auth.go             # Cookie session 认证中间件
│       └── handler/
│           ├── auth.go             # POST /login, POST /logout
│           ├── vm.go               # VM CRUD + 操作 handlers
│           └── task.go             # GET /tasks/:id
├── templates/
│   └── vm-domain.xml.tmpl          # libvirt domain XML 模板
└── web/
    ├── index.html                  # 单页应用 shell
    ├── app.js                      # hash router + 所有视图逻辑
    └── style.css                   # 自定义样式覆盖
```

---

## Task 1: 项目初始化

**Files:**
- Create: `go.mod`
- Create: `config.yaml`

- [ ] **Step 1: 初始化 Go module**

```bash
cd /path/to/broodvm
go mod init github.com/dujianqiang/broodvm
```

- [ ] **Step 2: 安装依赖**

```bash
go get github.com/gin-gonic/gin@v1.9.1
go get libvirt.org/go/libvirt@v1.10005.0
go get modernc.org/sqlite@v1.30.0
go get gopkg.in/yaml.v3@v3.0.1
go get github.com/google/uuid@v1.6.0
go mod tidy
```

- [ ] **Step 3: 创建默认配置文件**

```yaml
# config.yaml
host:
  bridge: br0
  image_dir: /var/lib/libvirt/images
  seed_image:
    source: "https://cloud-images.ubuntu.com/jammy/current/jammy-server-cloudimg-amd64.img"
  ssh_key: /root/.ssh/id_ed25519

server:
  port: 8080

auth:
  username: admin
  password: admin
```

- [ ] **Step 4: 创建目录结构**

```bash
mkdir -p internal/{config,store,libvirt,cloudinit,service,api/{middleware,handler}}
mkdir -p templates web
```

- [ ] **Step 5: Commit**

```bash
git add go.mod go.sum config.yaml
git commit -m "chore: init go module and config"
```

---

## Task 2: 配置加载

**Files:**
- Create: `internal/config/config.go`
- Create: `internal/config/config_test.go`

- [ ] **Step 1: 写失败测试**

```go
// internal/config/config_test.go
package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/dujianqiang/broodvm/internal/config"
)

func TestLoad(t *testing.T) {
	yaml := `
host:
  bridge: br0
  image_dir: /tmp/images
  seed_image:
    source: /tmp/seed.img
  ssh_key: /root/.ssh/id_ed25519
server:
  port: 9090
auth:
  username: admin
  password: secret
`
	f := filepath.Join(t.TempDir(), "config.yaml")
	os.WriteFile(f, []byte(yaml), 0644)

	cfg, err := config.Load(f)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Host.Bridge != "br0" {
		t.Errorf("bridge = %q, want br0", cfg.Host.Bridge)
	}
	if cfg.Server.Port != 9090 {
		t.Errorf("port = %d, want 9090", cfg.Server.Port)
	}
	if cfg.Auth.Password != "secret" {
		t.Errorf("password = %q, want secret", cfg.Auth.Password)
	}
}

func TestEnsureSeedImage_LocalMissing(t *testing.T) {
	cfg := &config.Config{}
	cfg.Host.SeedImage.Source = "/nonexistent/seed.img"
	cfg.Host.ImageDir = t.TempDir()

	_, err := config.EnsureSeedImage(cfg)
	if err == nil {
		t.Fatal("expected error for missing local file")
	}
}

func TestEnsureSeedImage_LocalExists(t *testing.T) {
	dir := t.TempDir()
	seedPath := filepath.Join(dir, "seed.img")
	os.WriteFile(seedPath, []byte("fake"), 0644)

	cfg := &config.Config{}
	cfg.Host.SeedImage.Source = seedPath
	cfg.Host.ImageDir = dir

	got, err := config.EnsureSeedImage(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != seedPath {
		t.Errorf("got %q, want %q", got, seedPath)
	}
}
```

- [ ] **Step 2: 运行确认测试失败**

```bash
go test ./internal/config/...
```

预期：`config_test.go: cannot find package`

- [ ] **Step 3: 实现 config.go**

```go
// internal/config/config.go
package config

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Host struct {
		Bridge    string `yaml:"bridge"`
		ImageDir  string `yaml:"image_dir"`
		SeedImage struct {
			Source string `yaml:"source"`
		} `yaml:"seed_image"`
		SSHKey string `yaml:"ssh_key"`
	} `yaml:"host"`
	Server struct {
		Port int `yaml:"port"`
	} `yaml:"server"`
	Auth struct {
		Username string `yaml:"username"`
		Password string `yaml:"password"`
	} `yaml:"auth"`
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	return &cfg, nil
}

// EnsureSeedImage 确保种子镜像在本地存在，返回本地路径。
// URL 来源：若 image_dir 下已有同名文件则跳过，否则下载。
// 本地路径：验证文件存在，不存在则返回错误。
func EnsureSeedImage(cfg *Config) (string, error) {
	src := cfg.Host.SeedImage.Source
	imageDir := cfg.Host.ImageDir

	if strings.HasPrefix(src, "http://") || strings.HasPrefix(src, "https://") {
		filename := filepath.Base(src)
		localPath := filepath.Join(imageDir, filename)
		if _, err := os.Stat(localPath); err == nil {
			return localPath, nil
		}
		if err := downloadFile(localPath, src); err != nil {
			return "", fmt.Errorf("download seed image: %w", err)
		}
		return localPath, nil
	}

	if _, err := os.Stat(src); err != nil {
		return "", fmt.Errorf("seed image not found at %s", src)
	}
	return src, nil
}

func downloadFile(dest, url string) error {
	resp, err := http.Get(url) //nolint:gosec
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(f, resp.Body)
	return err
}
```

- [ ] **Step 4: 运行确认测试通过**

```bash
go test ./internal/config/... -v
```

预期：`PASS`（TestLoad + TestEnsureSeedImage_LocalMissing + TestEnsureSeedImage_LocalExists 全绿）

- [ ] **Step 5: Commit**

```bash
git add internal/config/
git commit -m "feat: add config loading with seed image resolution"
```

---

## Task 3: 数据库初始化 + Store

**Files:**
- Create: `internal/store/db.go`
- Create: `internal/store/vm.go`
- Create: `internal/store/task.go`
- Create: `internal/store/store_test.go`

- [ ] **Step 1: 写失败测试**

```go
// internal/store/store_test.go
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
```

- [ ] **Step 2: 运行确认测试失败**

```bash
go test ./internal/store/...
```

预期：`cannot find package`

- [ ] **Step 3: 实现 db.go**

```go
// internal/store/db.go
package store

import (
	"database/sql"
	_ "modernc.org/sqlite"
)

type DB struct{ *sql.DB }

func Open(path string) (*DB, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1) // SQLite 不支持并发写
	if err := migrate(db); err != nil {
		db.Close()
		return nil, err
	}
	return &DB{db}, nil
}

func migrate(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS vms (
			id           TEXT PRIMARY KEY,
			name         TEXT NOT NULL UNIQUE,
			vcpu         INTEGER NOT NULL,
			memory_gb    INTEGER NOT NULL,
			disk_gb      INTEGER NOT NULL,
			network_type TEXT NOT NULL,
			mac          TEXT NOT NULL,
			vnc_port     INTEGER NOT NULL,
			ip           TEXT NOT NULL DEFAULT '',
			status       TEXT NOT NULL,
			created_at   TEXT NOT NULL
		);
		CREATE TABLE IF NOT EXISTS tasks (
			id         TEXT PRIMARY KEY,
			type       TEXT NOT NULL,
			ref_id     TEXT NOT NULL,
			status     TEXT NOT NULL,
			progress   INTEGER NOT NULL DEFAULT 0,
			message    TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL
		);
	`)
	return err
}
```

- [ ] **Step 4: 实现 vm.go**

```go
// internal/store/vm.go
package store

import (
	"database/sql"
	"time"
)

type VM struct {
	ID          string
	Name        string
	VCPU        int
	MemoryGB    int
	DiskGB      int
	NetworkType string
	MAC         string
	VNCPort     int
	IP          string
	Status      string
	CreatedAt   time.Time
}

type VMStore struct{ db *DB }

func NewVMStore(db *DB) *VMStore { return &VMStore{db: db} }

func (s *VMStore) Create(vm *VM) error {
	_, err := s.db.Exec(
		`INSERT INTO vms (id,name,vcpu,memory_gb,disk_gb,network_type,mac,vnc_port,ip,status,created_at)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		vm.ID, vm.Name, vm.VCPU, vm.MemoryGB, vm.DiskGB,
		vm.NetworkType, vm.MAC, vm.VNCPort, vm.IP, vm.Status,
		vm.CreatedAt.Format(time.RFC3339),
	)
	return err
}

func (s *VMStore) Get(id string) (*VM, error) {
	row := s.db.QueryRow(
		`SELECT id,name,vcpu,memory_gb,disk_gb,network_type,mac,vnc_port,ip,status,created_at
		 FROM vms WHERE id=?`, id)
	return scanVM(row)
}

func (s *VMStore) List() ([]*VM, error) {
	rows, err := s.db.Query(
		`SELECT id,name,vcpu,memory_gb,disk_gb,network_type,mac,vnc_port,ip,status,created_at
		 FROM vms WHERE status != 'deleted' ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var vms []*VM
	for rows.Next() {
		vm, err := scanVM(rows)
		if err != nil {
			return nil, err
		}
		vms = append(vms, vm)
	}
	return vms, rows.Err()
}

func (s *VMStore) UpdateStatus(id, status, ip string) error {
	_, err := s.db.Exec(`UPDATE vms SET status=?, ip=? WHERE id=?`, status, ip, id)
	return err
}

func (s *VMStore) Delete(id string) error {
	_, err := s.db.Exec(`UPDATE vms SET status='deleted' WHERE id=?`, id)
	return err
}

// NextVNCPort 返回下一个可用 VNC 端口（从 5900 开始）。
func (s *VMStore) NextVNCPort() (int, error) {
	row := s.db.QueryRow(
		`SELECT COALESCE(MAX(vnc_port), 5899) FROM vms WHERE status != 'deleted'`)
	var maxPort int
	if err := row.Scan(&maxPort); err != nil {
		return 0, err
	}
	return maxPort + 1, nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanVM(s rowScanner) (*VM, error) {
	var vm VM
	var createdAt string
	err := s.Scan(
		&vm.ID, &vm.Name, &vm.VCPU, &vm.MemoryGB, &vm.DiskGB,
		&vm.NetworkType, &vm.MAC, &vm.VNCPort, &vm.IP, &vm.Status, &createdAt,
	)
	if err != nil {
		return nil, err
	}
	vm.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	return &vm, nil
}
```

- [ ] **Step 5: 实现 task.go**

```go
// internal/store/task.go
package store

import "time"

type Task struct {
	ID        string
	Type      string
	RefID     string
	Status    string
	Progress  int
	Message   string
	CreatedAt time.Time
}

type TaskStore struct{ db *DB }

func NewTaskStore(db *DB) *TaskStore { return &TaskStore{db: db} }

func (s *TaskStore) Create(t *Task) error {
	_, err := s.db.Exec(
		`INSERT INTO tasks (id,type,ref_id,status,progress,message,created_at)
		 VALUES (?,?,?,?,?,?,?)`,
		t.ID, t.Type, t.RefID, t.Status, t.Progress, t.Message,
		t.CreatedAt.Format(time.RFC3339),
	)
	return err
}

func (s *TaskStore) Get(id string) (*Task, error) {
	row := s.db.QueryRow(
		`SELECT id,type,ref_id,status,progress,message,created_at FROM tasks WHERE id=?`, id)
	var t Task
	var createdAt string
	err := row.Scan(&t.ID, &t.Type, &t.RefID, &t.Status, &t.Progress, &t.Message, &createdAt)
	if err != nil {
		return nil, err
	}
	t.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	return &t, nil
}

func (s *TaskStore) Update(id, status string, progress int, message string) error {
	_, err := s.db.Exec(
		`UPDATE tasks SET status=?, progress=?, message=? WHERE id=?`,
		status, progress, message, id,
	)
	return err
}
```

- [ ] **Step 6: 运行确认测试通过**

```bash
go test ./internal/store/... -v
```

预期：所有 5 个测试 `PASS`

- [ ] **Step 7: Commit**

```bash
git add internal/store/
git commit -m "feat: add SQLite store for vms and tasks"
```

---

## Task 4: libvirt 客户端接口 + 实现

**Files:**
- Create: `internal/libvirt/client.go`
- Create: `internal/libvirt/mock.go`

> 注意：`internal/libvirt/client.go` 使用 CGo，需宿主机安装 `libvirt-dev`。此 package 无单元测试，集成测试依赖真实 libvirtd。

- [ ] **Step 1: 实现 Client 接口**

```go
// internal/libvirt/client.go
package libvirt

import (
	"fmt"

	golibvirt "libvirt.org/go/libvirt"
)

// Client 定义 BroodVM 所需的 libvirt 操作。
type Client interface {
	DefineAndStart(xmlDesc string) error
	Destroy(name string) error   // 强制关机（virsh destroy）
	Undefine(name string) error  // 删除定义（virsh undefine）
	Start(name string) error     // 启动已停止的 VM
	Shutdown(name string) error  // 优雅关机
	Reboot(name string) error
	IsRunning(name string) (bool, error)
	GetIP(name string) (string, error) // 返回第一个非回环 IPv4，未就绪时返回 ""
}

type LibvirtClient struct {
	conn *golibvirt.Connect
}

func NewLibvirtClient(uri string) (*LibvirtClient, error) {
	conn, err := golibvirt.NewConnect(uri)
	if err != nil {
		return nil, fmt.Errorf("libvirt connect %s: %w", uri, err)
	}
	return &LibvirtClient{conn: conn}, nil
}

func (c *LibvirtClient) DefineAndStart(xmlDesc string) error {
	dom, err := c.conn.DomainDefineXML(xmlDesc)
	if err != nil {
		return fmt.Errorf("define domain: %w", err)
	}
	defer dom.Free()
	return dom.Create()
}

func (c *LibvirtClient) Destroy(name string) error {
	dom, err := c.conn.LookupDomainByName(name)
	if err != nil {
		return err
	}
	defer dom.Free()
	return dom.Destroy()
}

func (c *LibvirtClient) Undefine(name string) error {
	dom, err := c.conn.LookupDomainByName(name)
	if err != nil {
		return err
	}
	defer dom.Free()
	return dom.Undefine()
}

func (c *LibvirtClient) Start(name string) error {
	dom, err := c.conn.LookupDomainByName(name)
	if err != nil {
		return err
	}
	defer dom.Free()
	return dom.Create()
}

func (c *LibvirtClient) Shutdown(name string) error {
	dom, err := c.conn.LookupDomainByName(name)
	if err != nil {
		return err
	}
	defer dom.Free()
	return dom.Shutdown()
}

func (c *LibvirtClient) Reboot(name string) error {
	dom, err := c.conn.LookupDomainByName(name)
	if err != nil {
		return err
	}
	defer dom.Free()
	return dom.Reboot(0)
}

func (c *LibvirtClient) IsRunning(name string) (bool, error) {
	dom, err := c.conn.LookupDomainByName(name)
	if err != nil {
		return false, err
	}
	defer dom.Free()
	state, _, err := dom.GetState()
	if err != nil {
		return false, err
	}
	return state == golibvirt.DOMAIN_RUNNING, nil
}

func (c *LibvirtClient) GetIP(name string) (string, error) {
	dom, err := c.conn.LookupDomainByName(name)
	if err != nil {
		return "", err
	}
	defer dom.Free()
	ifaces, err := dom.InterfaceAddresses(golibvirt.DOMAIN_INTERFACE_ADDRESSES_SRC_LEASE, 0)
	if err != nil {
		return "", nil // dnsmasq lease 可能还未就绪，不算错误
	}
	for _, iface := range ifaces {
		for _, addr := range iface.Addrs {
			if addr.Type == golibvirt.IP_ADDR_TYPE_IPV4 && addr.Addr != "127.0.0.1" {
				return addr.Addr, nil
			}
		}
	}
	return "", nil
}
```

- [ ] **Step 2: 实现 MockClient（供测试使用）**

```go
// internal/libvirt/mock.go
package libvirt

// MockClient 实现 Client 接口，用于单元测试。
type MockClient struct {
	RunningDomains map[string]bool
	DomainIPs      map[string]string

	// 可预设每个方法返回的错误
	DefineAndStartErr error
	DestroyErr        error
	UndefineErr       error
	StartErr          error
	ShutdownErr       error
	RebootErr         error
}

func NewMock() *MockClient {
	return &MockClient{
		RunningDomains: make(map[string]bool),
		DomainIPs:      make(map[string]string),
	}
}

func (m *MockClient) DefineAndStart(xmlDesc string) error {
	if m.DefineAndStartErr != nil {
		return m.DefineAndStartErr
	}
	return nil
}

func (m *MockClient) Destroy(name string) error {
	if m.DestroyErr != nil {
		return m.DestroyErr
	}
	delete(m.RunningDomains, name)
	return nil
}

func (m *MockClient) Undefine(name string) error { return m.UndefineErr }

func (m *MockClient) Start(name string) error {
	if m.StartErr != nil {
		return m.StartErr
	}
	m.RunningDomains[name] = true
	return nil
}

func (m *MockClient) Shutdown(name string) error {
	if m.ShutdownErr != nil {
		return m.ShutdownErr
	}
	delete(m.RunningDomains, name)
	return nil
}

func (m *MockClient) Reboot(name string) error { return m.RebootErr }

func (m *MockClient) IsRunning(name string) (bool, error) {
	return m.RunningDomains[name], nil
}

func (m *MockClient) GetIP(name string) (string, error) {
	return m.DomainIPs[name], nil
}
```

- [ ] **Step 3: 编译确认（不跑测试，CGo 需要宿主机）**

```bash
go build ./internal/libvirt/...
```

预期：无编译错误

- [ ] **Step 4: Commit**

```bash
git add internal/libvirt/
git commit -m "feat: add libvirt client interface and mock"
```

---

## Task 5: Cloud-init ISO 生成

**Files:**
- Create: `internal/cloudinit/gen.go`

> 依赖宿主机 `genisoimage`（`apt install genisoimage`）。此包无单元测试，集成时手工验证。

- [ ] **Step 1: 实现 gen.go**

```go
// internal/cloudinit/gen.go
package cloudinit

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Generate 在 destDir 下生成 <vmName>-cidata.iso，返回 ISO 路径。
// sshPubKey 为 SSH 公钥内容（如 "ssh-ed25519 AAAA..."）。
func Generate(destDir, vmName, sshPubKey string) (string, error) {
	tmpDir, err := os.MkdirTemp("", "cidata-"+vmName+"-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmpDir)

	metaData := fmt.Sprintf("instance-id: %s\nlocal-hostname: %s\n", vmName, vmName)
	userData := fmt.Sprintf(`#cloud-config
users:
  - name: ubuntu
    sudo: ALL=(ALL) NOPASSWD:ALL
    shell: /bin/bash
    lock_passwd: true
    ssh_authorized_keys:
      - %s
`, strings.TrimSpace(sshPubKey))

	if err := os.WriteFile(filepath.Join(tmpDir, "meta-data"), []byte(metaData), 0644); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "user-data"), []byte(userData), 0644); err != nil {
		return "", err
	}

	isoPath := filepath.Join(destDir, vmName+"-cidata.iso")
	out, err := exec.Command("genisoimage",
		"-output", isoPath,
		"-volid", "cidata",
		"-joliet", "-rock",
		filepath.Join(tmpDir, "meta-data"),
		filepath.Join(tmpDir, "user-data"),
	).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("genisoimage failed: %w\n%s", err, out)
	}
	return isoPath, nil
}
```

- [ ] **Step 2: 编译确认**

```bash
go build ./internal/cloudinit/...
```

- [ ] **Step 3: Commit**

```bash
git add internal/cloudinit/
git commit -m "feat: add cloud-init cidata ISO generator"
```

---

## Task 6: VM Domain XML 模板

**Files:**
- Create: `templates/vm-domain.xml.tmpl`

- [ ] **Step 1: 创建模板文件**

```xml
<!-- templates/vm-domain.xml.tmpl -->
<domain type='kvm'>
  <name>{{.Name}}</name>
  <uuid>{{.UUID}}</uuid>
  <memory unit='GiB'>{{.MemoryGB}}</memory>
  <currentMemory unit='GiB'>{{.MemoryGB}}</currentMemory>
  <vcpu placement='static'>{{.VCPU}}</vcpu>
  <os>
    <type arch='x86_64' machine='q35'>hvm</type>
    <boot dev='hd'/>
  </os>
  <features>
    <acpi/>
    <apic/>
  </features>
  <cpu mode='host-passthrough' check='none'/>
  <clock offset='utc'>
    <timer name='rtc' tickpolicy='catchup'/>
    <timer name='pit' tickpolicy='delay'/>
    <timer name='hpet' present='no'/>
  </clock>
  <devices>
    <emulator>/usr/bin/qemu-system-x86_64</emulator>
    <disk type='file' device='disk'>
      <driver name='qemu' type='qcow2' discard='unmap'/>
      <source file='{{.DiskPath}}'/>
      <target dev='vda' bus='virtio'/>
    </disk>
    <disk type='file' device='cdrom'>
      <driver name='qemu' type='raw'/>
      <source file='{{.CidataPath}}'/>
      <target dev='sda' bus='sata'/>
      <readonly/>
    </disk>
    {{- if eq .NetworkType "bridge"}}
    <interface type='bridge'>
      <mac address='{{.MAC}}'/>
      <source bridge='{{.Bridge}}'/>
      <model type='virtio'/>
    </interface>
    {{- else}}
    <interface type='network'>
      <mac address='{{.MAC}}'/>
      <source network='default'/>
      <model type='virtio'/>
    </interface>
    {{- end}}
    <serial type='pty'>
      <target type='isa-serial' port='0'/>
    </serial>
    <console type='pty'>
      <target type='serial' port='0'/>
    </console>
    <graphics type='vnc' port='{{.VNCPort}}' listen='0.0.0.0' keymap='en-us'/>
    <video>
      <model type='vga' vram='16384'/>
    </video>
  </devices>
</domain>
```

- [ ] **Step 2: Commit**

```bash
git add templates/
git commit -m "feat: add libvirt domain XML template"
```

---

## Task 7: Session 存储

**Files:**
- Create: `internal/service/session.go`

- [ ] **Step 1: 写失败测试**

新建 `internal/service/session_test.go`：

```go
// internal/service/session_test.go
package service_test

import (
	"testing"

	"github.com/dujianqiang/broodvm/internal/service"
)

func TestSessionStore(t *testing.T) {
	s := service.NewSessionStore()

	token := s.Create("admin")
	if token == "" {
		t.Fatal("token should not be empty")
	}

	if !s.Validate(token) {
		t.Error("valid token should validate")
	}
	if s.Validate("bad-token") {
		t.Error("bad token should not validate")
	}

	s.Delete(token)
	if s.Validate(token) {
		t.Error("deleted token should not validate")
	}
}
```

- [ ] **Step 2: 运行确认失败**

```bash
go test ./internal/service/...
```

- [ ] **Step 3: 实现 session.go**

```go
// internal/service/session.go
package service

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
)

type SessionStore struct {
	mu       sync.RWMutex
	sessions map[string]string // token -> username
}

func NewSessionStore() *SessionStore {
	return &SessionStore{sessions: make(map[string]string)}
}

func (s *SessionStore) Create(username string) string {
	b := make([]byte, 16)
	rand.Read(b) //nolint:errcheck
	token := hex.EncodeToString(b)
	s.mu.Lock()
	s.sessions[token] = username
	s.mu.Unlock()
	return token
}

func (s *SessionStore) Validate(token string) bool {
	s.mu.RLock()
	_, ok := s.sessions[token]
	s.mu.RUnlock()
	return ok
}

func (s *SessionStore) Delete(token string) {
	s.mu.Lock()
	delete(s.sessions, token)
	s.mu.Unlock()
}
```

- [ ] **Step 4: 运行确认测试通过**

```bash
go test ./internal/service/... -v -run TestSessionStore
```

预期：`PASS`

- [ ] **Step 5: Commit**

```bash
git add internal/service/session.go internal/service/session_test.go
git commit -m "feat: add in-memory session store"
```

---

## Task 8: VM Service

**Files:**
- Create: `internal/service/vm.go`
- Create: `internal/service/vm_test.go`

- [ ] **Step 1: 写失败测试**

```go
// internal/service/vm_test.go
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

	// 先创建一条 VM 记录（status=stopped）
	vm := &store.VM{
		ID: "vm-id-1", Name: "vm-test",
		VCPU: 1, MemoryGB: 1, DiskGB: 10,
		NetworkType: "nat", MAC: "52:54:00:aa:bb:01",
		VNCPort: 5900, Status: "stopped", CreatedAt: time.Now().UTC(),
	}
	vs.Create(vm)

	if err := svc.Start("vm-id-1"); err != nil {
		t.Fatalf("Start: %v", err)
	}
	got, _ := vs.Get("vm-id-1")
	if got.Status != "running" {
		t.Errorf("status = %q, want running", got.Status)
	}
	if !mock.RunningDomains["vm-test"] {
		t.Error("mock: domain should be marked as running")
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

	if err := svc.Stop("vm-id-2"); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	got, _ := vs.Get("vm-id-2")
	if got.Status != "stopped" {
		t.Errorf("status = %q, want stopped", got.Status)
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

	// VM 记录应以 creating 状态存在
	vms, _ := vs.List()
	if len(vms) != 1 || vms[0].Name != "vm-new" || vms[0].Status != "creating" {
		t.Errorf("unexpected vms: %+v", vms)
	}

	// Task 记录应存在
	task, err := ts.Get(taskID)
	if err != nil {
		t.Fatalf("Get task: %v", err)
	}
	if task.Type != "create_vm" {
		t.Errorf("task type = %q, want create_vm", task.Type)
	}
}
```

- [ ] **Step 2: 运行确认失败**

```bash
go test ./internal/service/... -run TestVMService
```

- [ ] **Step 3: 实现 vm.go**

```go
// internal/service/vm.go
package service

import (
	"bytes"
	"crypto/rand"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/template"
	"time"

	"github.com/google/uuid"
	"github.com/dujianqiang/broodvm/internal/cloudinit"
	"github.com/dujianqiang/broodvm/internal/config"
	lv "github.com/dujianqiang/broodvm/internal/libvirt"
	"github.com/dujianqiang/broodvm/internal/store"
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
	Name        string
	VCPU        int
	MemoryGB    int
	DiskGB      int
	NetworkType string
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

// ListVMs 返回所有非删除的 VM。
func (s *VMService) ListVMs() ([]*store.VM, error) { return s.vmStore.List() }

// GetVM 返回单个 VM。
func (s *VMService) GetVM(id string) (*store.VM, error) { return s.vmStore.Get(id) }

// GetTask 返回任务状态。
func (s *VMService) GetTask(id string) (*store.Task, error) { return s.taskStore.Get(id) }

// Unused import guard
var _ = strings.TrimSpace
```

- [ ] **Step 4: 运行确认测试通过**

```bash
go test ./internal/service/... -v -run TestVMService
```

预期：3 个测试 `PASS`

- [ ] **Step 5: Commit**

```bash
git add internal/service/vm.go internal/service/vm_test.go
git commit -m "feat: add VMService with async create/delete and sync start/stop/restart"
```

---

## Task 9: 认证中间件 + Auth Handler

**Files:**
- Create: `internal/api/middleware/auth.go`
- Create: `internal/api/handler/auth.go`

- [ ] **Step 1: 写失败测试**

```go
// internal/api/handler/auth_test.go
package handler_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/dujianqiang/broodvm/internal/api/handler"
	"github.com/dujianqiang/broodvm/internal/api/middleware"
	"github.com/dujianqiang/broodvm/internal/config"
	"github.com/dujianqiang/broodvm/internal/service"
)

func init() { gin.SetMode(gin.TestMode) }

func newAuthRouter() (*gin.Engine, *service.SessionStore) {
	cfg := &config.Config{}
	cfg.Auth.Username = "admin"
	cfg.Auth.Password = "secret"
	sessions := service.NewSessionStore()
	h := handler.NewAuthHandler(cfg, sessions)
	r := gin.New()
	r.POST("/api/login", h.Login)
	r.POST("/api/logout", middleware.Auth(sessions), h.Logout)
	return r, sessions
}

func TestLogin_Success(t *testing.T) {
	r, _ := newAuthRouter()
	body, _ := json.Marshal(map[string]string{"username": "admin", "password": "secret"})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	cookies := w.Result().Cookies()
	var found bool
	for _, c := range cookies {
		if c.Name == "session" && c.Value != "" {
			found = true
		}
	}
	if !found {
		t.Error("session cookie not set")
	}
}

func TestLogin_WrongPassword(t *testing.T) {
	r, _ := newAuthRouter()
	body, _ := json.Marshal(map[string]string{"username": "admin", "password": "wrong"})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
}

func TestMiddleware_Unauthorized(t *testing.T) {
	r, _ := newAuthRouter()
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/logout", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
}
```

- [ ] **Step 2: 运行确认失败**

```bash
go test ./internal/api/handler/... -run TestLogin
```

- [ ] **Step 3: 实现 middleware/auth.go**

```go
// internal/api/middleware/auth.go
package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/dujianqiang/broodvm/internal/service"
)

func Auth(sessions *service.SessionStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		token, err := c.Cookie("session")
		if err != nil || !sessions.Validate(token) {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "未登录"})
			c.Abort()
			return
		}
		c.Next()
	}
}
```

- [ ] **Step 4: 实现 handler/auth.go**

```go
// internal/api/handler/auth.go
package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/dujianqiang/broodvm/internal/config"
	"github.com/dujianqiang/broodvm/internal/service"
)

type AuthHandler struct {
	cfg      *config.Config
	sessions *service.SessionStore
}

func NewAuthHandler(cfg *config.Config, sessions *service.SessionStore) *AuthHandler {
	return &AuthHandler{cfg: cfg, sessions: sessions}
}

func (h *AuthHandler) Login(c *gin.Context) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
		return
	}
	if req.Username != h.cfg.Auth.Username || req.Password != h.cfg.Auth.Password {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "用户名或密码错误"})
		return
	}
	token := h.sessions.Create(req.Username)
	c.SetCookie("session", token, 86400, "/", "", false, true)
	c.JSON(http.StatusOK, gin.H{"message": "登录成功"})
}

func (h *AuthHandler) Logout(c *gin.Context) {
	token, _ := c.Cookie("session")
	if token != "" {
		h.sessions.Delete(token)
	}
	c.SetCookie("session", "", -1, "/", "", false, true)
	c.JSON(http.StatusOK, gin.H{"message": "已登出"})
}
```

- [ ] **Step 5: 运行确认测试通过**

```bash
go test ./internal/api/... -v -run "TestLogin|TestMiddleware"
```

预期：3 个测试 `PASS`

- [ ] **Step 6: Commit**

```bash
git add internal/api/middleware/ internal/api/handler/auth.go internal/api/handler/auth_test.go
git commit -m "feat: add cookie session auth middleware and login/logout handlers"
```

---

## Task 10: VM Handler + Task Handler

**Files:**
- Create: `internal/api/handler/vm.go`
- Create: `internal/api/handler/task.go`

- [ ] **Step 1: 写失败测试**

```go
// internal/api/handler/vm_test.go
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

	sessions := service.NewSessionStore()
	token := sessions.Create("admin")

	vmH := handler.NewVMHandler(svc)
	taskH := handler.NewTaskHandler(ts)

	r := gin.New()
	// 测试环境跳过 middleware，直接注册
	r.GET("/api/vms", vmH.List)
	r.POST("/api/vms", vmH.Create)
	r.GET("/api/vms/:id", vmH.Get)
	r.DELETE("/api/vms/:id", vmH.Delete)
	r.POST("/api/vms/:id/start", vmH.Start)
	r.POST("/api/vms/:id/stop", vmH.Stop)
	r.POST("/api/vms/:id/restart", vmH.Restart)
	r.GET("/api/tasks/:task_id", taskH.Get)

	// 将 token 写入 cookie helper
	_ = token // 测试环境直接注册路由，不走 middleware
	_ = sessions

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
		"name": "vm-new", "vcpu": 2, "memory_gb": 4, "disk_gb": 20, "network_type": "nat",
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
```

- [ ] **Step 2: 运行确认失败**

```bash
go test ./internal/api/handler/... -run TestVMHandler
```

- [ ] **Step 3: 实现 handler/vm.go**

```go
// internal/api/handler/vm.go
package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/dujianqiang/broodvm/internal/service"
)

type VMHandler struct {
	svc *service.VMService
}

func NewVMHandler(svc *service.VMService) *VMHandler {
	return &VMHandler{svc: svc}
}

func (h *VMHandler) List(c *gin.Context) {
	vms, err := h.svc.ListVMs()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if vms == nil {
		vms = []*store.VM{} // 避免返回 null
	}
	c.JSON(http.StatusOK, vms)
}

func (h *VMHandler) Create(c *gin.Context) {
	var req service.CreateVMReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误: " + err.Error()})
		return
	}
	taskID, err := h.svc.Create(req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"task_id": taskID})
}

func (h *VMHandler) Get(c *gin.Context) {
	vm, err := h.svc.GetVM(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "VM 不存在"})
		return
	}
	c.JSON(http.StatusOK, vm)
}

func (h *VMHandler) Delete(c *gin.Context) {
	taskID, err := h.svc.Delete(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"task_id": taskID})
}

func (h *VMHandler) Start(c *gin.Context) {
	if err := h.svc.Start(c.Param("id")); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "已启动"})
}

func (h *VMHandler) Stop(c *gin.Context) {
	if err := h.svc.Stop(c.Param("id")); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "已关机"})
}

func (h *VMHandler) Restart(c *gin.Context) {
	if err := h.svc.Restart(c.Param("id")); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "已重启"})
}
```

注意：`List` 中引用了 `store.VM`，需要在文件顶部补充 import：

```go
import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/dujianqiang/broodvm/internal/service"
	"github.com/dujianqiang/broodvm/internal/store"
)
```

- [ ] **Step 4: 实现 handler/task.go**

```go
// internal/api/handler/task.go
package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/dujianqiang/broodvm/internal/store"
)

type TaskHandler struct {
	taskStore *store.TaskStore
}

func NewTaskHandler(taskStore *store.TaskStore) *TaskHandler {
	return &TaskHandler{taskStore: taskStore}
}

func (h *TaskHandler) Get(c *gin.Context) {
	task, err := h.taskStore.Get(c.Param("task_id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "任务不存在"})
		return
	}
	c.JSON(http.StatusOK, task)
}
```

- [ ] **Step 5: 运行确认测试通过**

```bash
go test ./internal/api/handler/... -v -run TestVMHandler
```

预期：2 个测试 `PASS`

- [ ] **Step 6: Commit**

```bash
git add internal/api/handler/
git commit -m "feat: add VM and Task HTTP handlers"
```

---

## Task 11: Router

**Files:**
- Create: `internal/api/router.go`

- [ ] **Step 1: 实现 router.go**

```go
// internal/api/router.go
package api

import (
	"github.com/gin-gonic/gin"
	"github.com/dujianqiang/broodvm/internal/api/handler"
	"github.com/dujianqiang/broodvm/internal/api/middleware"
	"github.com/dujianqiang/broodvm/internal/service"
	"github.com/dujianqiang/broodvm/internal/store"
)

func NewRouter(
	sessions *service.SessionStore,
	authH *handler.AuthHandler,
	vmH *handler.VMHandler,
	taskH *handler.TaskHandler,
) *gin.Engine {
	r := gin.New()
	r.Use(gin.Logger(), gin.Recovery())

	api := r.Group("/api")
	api.POST("/login", authH.Login)
	api.POST("/logout", authH.Logout)

	protected := api.Group("", middleware.Auth(sessions))
	protected.GET("/vms", vmH.List)
	protected.POST("/vms", vmH.Create)
	protected.GET("/vms/:id", vmH.Get)
	protected.DELETE("/vms/:id", vmH.Delete)
	protected.POST("/vms/:id/start", vmH.Start)
	protected.POST("/vms/:id/stop", vmH.Stop)
	protected.POST("/vms/:id/restart", vmH.Restart)
	protected.GET("/tasks/:task_id", taskH.Get)

	return r
}

// unused import guard
var _ = store.VM{}
```

注意：`store` 未直接使用时去掉该行，仅保留真正需要的 import。

- [ ] **Step 2: 编译确认**

```bash
go build ./internal/api/...
```

- [ ] **Step 3: Commit**

```bash
git add internal/api/router.go
git commit -m "feat: add Gin router with auth-protected API routes"
```

---

## Task 12: 前端（index.html + app.js + style.css）

**Files:**
- Create: `web/index.html`
- Create: `web/app.js`
- Create: `web/style.css`

- [ ] **Step 1: 创建 index.html**

```html
<!-- web/index.html -->
<!DOCTYPE html>
<html lang="zh-CN" data-theme="light">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>BroodVM</title>
  <link rel="stylesheet" href="https://cdn.jsdelivr.net/npm/@picocss/pico@2/css/pico.min.css">
  <link rel="stylesheet" href="/style.css">
</head>
<body>
  <nav class="container-fluid">
    <ul><li><strong>BroodVM</strong></li></ul>
    <ul>
      <li><a href="#/" id="nav-home">VM 列表</a></li>
      <li><a href="#/vms/new">+ 新建</a></li>
      <li><button id="btn-logout" class="secondary outline">登出</button></li>
    </ul>
  </nav>
  <main class="container" id="app"></main>
  <script src="https://cdn.jsdelivr.net/npm/jquery@3.7.1/dist/jquery.min.js"></script>
  <script src="/app.js"></script>
</body>
</html>
```

- [ ] **Step 2: 创建 app.js**

```javascript
// web/app.js
const API = {
  login:   (u, p)  => $.post('/api/login',   JSON.stringify({username:u,password:p}), null, 'json'),
  logout:  ()      => $.post('/api/logout'),
  vms:     ()      => $.get('/api/vms'),
  vm:      (id)    => $.get(`/api/vms/${id}`),
  create:  (data)  => $.ajax({url:'/api/vms', method:'POST', contentType:'application/json', data:JSON.stringify(data)}),
  delete:  (id)    => $.ajax({url:`/api/vms/${id}`, method:'DELETE'}),
  start:   (id)    => $.post(`/api/vms/${id}/start`),
  stop:    (id)    => $.post(`/api/vms/${id}/stop`),
  restart: (id)    => $.post(`/api/vms/${id}/restart`),
  task:    (tid)   => $.get(`/api/tasks/${tid}`),
};

// 状态徽章
const badge = s => {
  const color = {running:'green',stopped:'gray',creating:'blue',error:'red'}[s] || 'gray';
  return `<span class="badge" style="background:${color}">${s}</span>`;
};

// 轮询 task 直到完成，回调 done(task)
function pollTask(taskId, onProgress, done) {
  const interval = setInterval(() => {
    API.task(taskId).done(t => {
      onProgress && onProgress(t);
      if (t.status === 'success' || t.status === 'failed') {
        clearInterval(interval);
        done(t);
      }
    }).fail(() => { clearInterval(interval); done({status:'failed',message:'网络错误'}); });
  }, 2000);
}

// 视图：登录
function renderLogin() {
  $('#app').html(`
    <article style="max-width:400px;margin:80px auto">
      <h2>登录 BroodVM</h2>
      <form id="form-login">
        <label>用户名<input id="inp-user" type="text" required></label>
        <label>密码<input id="inp-pass" type="password" required></label>
        <button type="submit">登录</button>
        <p id="login-err" style="color:red"></p>
      </form>
    </article>
  `);
  $('#form-login').on('submit', e => {
    e.preventDefault();
    API.login($('#inp-user').val(), $('#inp-pass').val())
      .done(() => navigate('/'))
      .fail(() => $('#login-err').text('用户名或密码错误'));
  });
}

// 视图：VM 列表
function renderList() {
  API.vms().done(vms => {
    if (!vms || !vms.length) {
      $('#app').html('<p>暂无虚拟机 <a href="#/vms/new">立即创建</a></p>');
      return;
    }
    const rows = vms.map(v => `
      <tr>
        <td>${v.Name}</td>
        <td>${v.VCPU}</td>
        <td>${v.MemoryGB} GiB</td>
        <td>${v.DiskGB} GiB</td>
        <td>${v.IP || '-'}</td>
        <td>${badge(v.Status)}</td>
        <td>
          <button class="btn-start outline secondary" data-id="${v.ID}" ${v.Status==='running'?'disabled':''}>启动</button>
          <button class="btn-stop outline secondary"  data-id="${v.ID}" ${v.Status!=='running'?'disabled':''}>停止</button>
          <button class="btn-restart outline secondary" data-id="${v.ID}" ${v.Status!=='running'?'disabled':''}>重启</button>
          <button class="btn-del contrast outline" data-id="${v.ID}">删除</button>
        </td>
      </tr>`).join('');
    $('#app').html(`
      <table>
        <thead><tr><th>名称</th><th>vCPU</th><th>内存</th><th>磁盘</th><th>IP</th><th>状态</th><th>操作</th></tr></thead>
        <tbody>${rows}</tbody>
      </table>
    `);
  }).fail(handleAuthFail);

  // 事件委托
  $('#app')
    .on('click', '.btn-start',   e => vmAction($(e.currentTarget).data('id'), 'start'))
    .on('click', '.btn-stop',    e => vmAction($(e.currentTarget).data('id'), 'stop'))
    .on('click', '.btn-restart', e => vmAction($(e.currentTarget).data('id'), 'restart'))
    .on('click', '.btn-del',     e => { if(confirm('确认删除？')) vmDelete($(e.currentTarget).data('id')); });
}

function vmAction(id, action) {
  API[action](id).done(() => renderList()).fail(r => alert(r.responseJSON?.error || '操作失败'));
}

function vmDelete(id) {
  API.delete(id).done(r => {
    alert('删除任务已提交，正在后台执行');
    pollTask(r.task_id, null, () => renderList());
  }).fail(r => alert(r.responseJSON?.error || '删除失败'));
}

// 视图：新建 VM
function renderNew() {
  $('#app').html(`
    <article style="max-width:500px">
      <h2>新建虚拟机</h2>
      <form id="form-create">
        <label>名称<input id="inp-name" type="text" placeholder="vm-001" required></label>
        <label>vCPU<input id="inp-vcpu" type="number" value="2" min="1" required></label>
        <label>内存 (GiB)<input id="inp-mem" type="number" value="4" min="1" required></label>
        <label>磁盘 (GiB)<input id="inp-disk" type="number" value="20" min="10" required></label>
        <label>网络类型
          <select id="inp-net">
            <option value="nat">NAT</option>
            <option value="bridge">Bridge</option>
          </select>
        </label>
        <button type="submit">创建</button>
        <a href="#/" role="button" class="secondary outline">取消</a>
      </form>
      <div id="progress-area" style="display:none">
        <progress id="prog-bar" value="0" max="100"></progress>
        <p id="prog-msg"></p>
      </div>
    </article>
  `);
  $('#form-create').on('submit', e => {
    e.preventDefault();
    const data = {
      Name: $('#inp-name').val(), VCPU: +$('#inp-vcpu').val(),
      MemoryGB: +$('#inp-mem').val(), DiskGB: +$('#inp-disk').val(),
      NetworkType: $('#inp-net').val(),
    };
    $('button[type=submit]').prop('disabled', true);
    API.create(data).done(r => {
      $('#progress-area').show();
      pollTask(r.task_id, t => {
        $('#prog-bar').val(t.progress);
        $('#prog-msg').text(t.message);
      }, t => {
        if (t.status === 'success') navigate('/');
        else { alert('创建失败：' + t.message); $('button[type=submit]').prop('disabled', false); }
      });
    }).fail(r => {
      alert(r.responseJSON?.error || '请求失败');
      $('button[type=submit]').prop('disabled', false);
    });
  });
}

// 视图：VM 详情
function renderVM(id) {
  API.vm(id).done(vm => {
    $('#app').html(`
      <article>
        <h2>${vm.Name}</h2>
        <table>
          <tr><th>ID</th><td>${vm.ID}</td></tr>
          <tr><th>状态</th><td>${badge(vm.Status)}</td></tr>
          <tr><th>vCPU</th><td>${vm.VCPU}</td></tr>
          <tr><th>内存</th><td>${vm.MemoryGB} GiB</td></tr>
          <tr><th>磁盘</th><td>${vm.DiskGB} GiB</td></tr>
          <tr><th>网络</th><td>${vm.NetworkType}</td></tr>
          <tr><th>IP</th><td>${vm.IP || '—'}</td></tr>
          <tr><th>VNC 端口</th><td>${vm.VNCPort}</td></tr>
          <tr><th>创建时间</th><td>${vm.CreatedAt}</td></tr>
        </table>
        <div class="grid">
          <button id="btn-start"   ${vm.Status==='running'?'disabled':''}>启动</button>
          <button id="btn-stop"    ${vm.Status!=='running'?'disabled':''}>停止</button>
          <button id="btn-restart" ${vm.Status!=='running'?'disabled':''}>重启</button>
          <button id="btn-del" class="contrast">删除</button>
        </div>
        <a href="#/">← 返回列表</a>
      </article>
    `);
    $('#btn-start').click(()   => vmAction(id, 'start'));
    $('#btn-stop').click(()    => vmAction(id, 'stop'));
    $('#btn-restart').click(() => vmAction(id, 'restart'));
    $('#btn-del').click(() => { if(confirm('确认删除？')) vmDelete(id); });
  }).fail(handleAuthFail);
}

function handleAuthFail(xhr) {
  if (xhr.status === 401) navigate('/login');
  else alert('请求失败: ' + xhr.status);
}

// Hash 路由
function navigate(path) { location.hash = '#' + path; }

function route() {
  const hash = location.hash.replace(/^#/, '') || '/';
  if (hash === '/login')     return renderLogin();
  if (hash === '/')          return renderList();
  if (hash === '/vms/new')   return renderNew();
  const m = hash.match(/^\/vms\/([^/]+)$/);
  if (m) return renderVM(m[1]);
  navigate('/');
}

$(document).ready(() => {
  $('#btn-logout').on('click', () => API.logout().always(() => navigate('/login')));
  $(window).on('hashchange', route);
  route();
});
```

- [ ] **Step 3: 创建 style.css**

```css
/* web/style.css */
.badge {
  display: inline-block;
  padding: 2px 8px;
  border-radius: 4px;
  color: #fff;
  font-size: 0.8em;
}

nav { border-bottom: 1px solid var(--pico-muted-border-color); margin-bottom: 1rem; }

table { width: 100%; }
th { white-space: nowrap; }
td button { margin: 0 2px; padding: 2px 8px; font-size: 0.85em; }
```

- [ ] **Step 4: Commit**

```bash
git add web/
git commit -m "feat: add frontend SPA with hash routing (login/list/create/detail)"
```

---

## Task 13: main.go 组装

**Files:**
- Create: `main.go`

- [ ] **Step 1: 实现 main.go**

```go
// main.go
package main

import (
	"embed"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"strings"
	"text/template"

	"github.com/gin-gonic/gin"
	"github.com/dujianqiang/broodvm/internal/api"
	"github.com/dujianqiang/broodvm/internal/api/handler"
	"github.com/dujianqiang/broodvm/internal/config"
	lv "github.com/dujianqiang/broodvm/internal/libvirt"
	"github.com/dujianqiang/broodvm/internal/service"
	"github.com/dujianqiang/broodvm/internal/store"
)

//go:embed templates web
var embeddedFS embed.FS

func main() {
	// 1. 加载配置
	cfg, err := config.Load("config.yaml")
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	// 2. 确保种子镜像存在
	seedImage, err := config.EnsureSeedImage(cfg)
	if err != nil {
		log.Fatalf("seed image: %v", err)
	}
	log.Printf("seed image: %s", seedImage)

	// 3. 打开数据库
	db, err := store.Open("broodvm.db")
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer db.Close()

	// 4. 初始化 store
	vmStore := store.NewVMStore(db)
	taskStore := store.NewTaskStore(db)

	// 5. 连接 libvirt
	virtClient, err := lv.NewLibvirtClient("qemu:///system")
	if err != nil {
		log.Fatalf("libvirt: %v", err)
	}

	// 6. 加载 VM XML 模板
	tmplContent, err := embeddedFS.ReadFile("templates/vm-domain.xml.tmpl")
	if err != nil {
		log.Fatalf("read template: %v", err)
	}
	vmTmpl, err := template.New("vm-domain").Parse(string(tmplContent))
	if err != nil {
		log.Fatalf("parse template: %v", err)
	}

	// 7. 初始化 service
	sessions := service.NewSessionStore()
	vmSvc := service.NewVMService(cfg, vmStore, taskStore, virtClient, seedImage)
	vmSvc.SetTemplate(vmTmpl)

	// 8. 初始化 handler
	authH := handler.NewAuthHandler(cfg, sessions)
	vmH := handler.NewVMHandler(vmSvc)
	taskH := handler.NewTaskHandler(taskStore)

	// 9. 设置路由
	r := api.NewRouter(sessions, authH, vmH, taskH)

	// 10. 挂载前端静态文件（SPA fallback）
	subFS, _ := fs.Sub(embeddedFS, "web")
	fileServer := http.FileServer(http.FS(subFS))
	r.NoRoute(func(c *gin.Context) {
		if strings.HasPrefix(c.Request.URL.Path, "/api") {
			c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
			return
		}
		// SPA：所有非 API 路由都返回 index.html
		c.Request.URL.Path = "/"
		fileServer.ServeHTTP(c.Writer, c.Request)
	})

	// 11. 启动
	addr := fmt.Sprintf(":%d", cfg.Server.Port)
	log.Printf("BroodVM listening on %s", addr)
	if err := r.Run(addr); err != nil {
		log.Fatalf("run: %v", err)
	}
}
```

- [ ] **Step 2: 编译确认**

```bash
go build -o broodvm .
```

预期：生成 `broodvm` 二进制，无编译错误

- [ ] **Step 3: 运行全量测试**

```bash
go test ./... -v
```

预期：所有单元测试通过（libvirt 集成测试需宿主机，跳过）

- [ ] **Step 4: Commit**

```bash
git add main.go
git commit -m "feat: wire up all components in main.go with embed.FS"
```

---

## Task 14: 集成验证（宿主机手动执行）

> 此任务需要在 Ubuntu 22.04 宿主机上执行，要求 KVM + libvirtd 已启动。

- [ ] **Step 1: 确认前置环境**

```bash
# 检查 libvirtd
systemctl status libvirtd

# 检查 KVM 模块
lsmod | grep kvm

# 检查工具
which qemu-img genisoimage virsh
```

- [ ] **Step 2: 配置 NAT 网络（若使用 nat 模式）**

```bash
virsh net-list --all
# 若 default 网络未激活：
virsh net-start default
virsh net-autostart default
```

- [ ] **Step 3: 启动 BroodVM**

```bash
# 编译（在宿主机上，需 libvirt-dev）
apt install -y libvirt-dev
go build -o broodvm .

# 运行
./broodvm
```

预期日志：`BroodVM listening on :8080`（首次运行若 source 为 URL，会自动下载种子镜像）

- [ ] **Step 4: 验证 API**

```bash
# 登录
curl -c cookies.txt -X POST http://localhost:8080/api/login \
  -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"admin"}'
# 预期：{"message":"登录成功"}

# 创建 VM
curl -b cookies.txt -X POST http://localhost:8080/api/vms \
  -H 'Content-Type: application/json' \
  -d '{"Name":"vm-001","VCPU":1,"MemoryGB":1,"DiskGB":10,"NetworkType":"nat"}'
# 预期：{"task_id":"<uuid>"}

# 轮询任务（替换 <task_id>）
curl -b cookies.txt http://localhost:8080/api/tasks/<task_id>
# 轮询直到 status=success

# 列表
curl -b cookies.txt http://localhost:8080/api/vms
```

- [ ] **Step 5: 验证 Web 界面**

打开浏览器访问 `http://<宿主机IP>:8080`，验证：
- 登录页面正常显示
- 登录后进入 VM 列表
- 新建 VM 表单可提交，进度条更新
- VM 列表显示新建的 VM，状态为 running
- 启动/停止/重启/删除按钮功能正常

- [ ] **Step 6: 最终 Commit**

```bash
git add .
git commit -m "chore: phase 1 complete — BroodVM basic VM lifecycle management"
```

---

## 自检：spec 覆盖确认

| Spec 要求 | 对应 Task |
|-----------|-----------|
| VM CRUD（创建/列表/详情/删除） | Task 8, 10 |
| 启动/停止/重启 | Task 8, 10 |
| 异步创建流程（qemu-img + cidata + virsh） | Task 8 |
| 异步删除流程（destroy + undefine + 清文件） | Task 8 |
| task 状态轮询 API | Task 10, 11 |
| SQLite 数据模型（vms + tasks 表） | Task 3 |
| config.yaml 加载 + 种子镜像逻辑 | Task 2 |
| libvirt Go binding | Task 4 |
| cloud-init cidata ISO | Task 5 |
| Cookie session 认证 | Task 7, 9 |
| 前端四个页面（登录/列表/新建/详情） | Task 12 |
| embed.FS 打包 | Task 13 |
| 单一二进制部署 | Task 13 |
