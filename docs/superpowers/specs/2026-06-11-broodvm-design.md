# BroodVM 设计文档

**日期：** 2026-06-11  
**项目：** BroodVM — KVM+QEMU 多实例虚拟化后台管理系统

---

## 实施范围说明

当前只实现基础功能，高级功能留待后续迭代。

| 阶段 | 内容 | 状态 |
|------|------|------|
| **Phase 1 — 基础功能** | VM CRUD + 启停重启、创建表单、异步任务进度、Web 后台 | **当前实现目标** |
| **Phase 2 — 扩展功能** | VM 标准模板、高级参数、宿主机能力检测、XML 编辑、软件安装、系统设置 | 后续迭代 |
| **Phase 3 — 运维** | `broodvm setup` 自动化初始化、启动自检 | 后续迭代 |

---

## 一、项目背景

基于 KVM/QEMU 的单宿主机虚拟机管理系统，提供 Web 后台界面，支持 VM 全生命周期管理（创建/启停/删除）。

- 宿主机：单台 Ubuntu 22.04
- 基础镜像：`jammy-server-cloudimg-amd64.img`
- VM 数量：由用户配置，无固定上限

---

## 二、技术选型

| 层 | 技术 | 说明 |
|----|------|------|
| HTTP 框架 | Gin | Go Web 框架 |
| VM 操作 | libvirt.org/go/libvirt | CGo binding，通过 Unix socket 与 libvirtd 通信 |
| 数据库 | SQLite（modernc.org/sqlite） | 纯 Go，无 CGo，单文件存储 |
| 前端 | jQuery + Pico.css | CDN 引入，零构建步骤 |
| 静态文件 | embed.FS | 前端打包进二进制 |

**部署产物：** 单个二进制 `broodvm` + `broodvm.db` + `config.yaml`

---

## 三、整体架构

```
┌─────────────────────────────────────────────┐
│                   BroodVM                    │
│                                              │
│  ┌──────────┐    ┌──────────┐               │
│  │  前端     │    │  REST    │               │
│  │ embed.FS │◄──►│ API(Gin) │               │
│  │ HTML/JS  │    └────┬─────┘               │
│  └──────────┘         │                     │
│                ┌──────┴──────┐              │
│                │             │              │
│          ┌─────▼────┐  ┌─────▼──────┐      │
│          │  SQLite  │  │  libvirt   │      │
│          │  元数据   │  │ Go binding │      │
│          └──────────┘  └─────┬──────┘      │
│                               │             │
└───────────────────────────────┼─────────────┘
                                │ Unix socket
                           ┌────▼────┐
                           │libvirtd │
                           └────┬────┘
                    ┌───────────┼───────────┐
                ┌───▼──┐   ┌───▼──┐   ┌───▼──┐
                │ VM-1 │   │ VM-2 │   │ VM-N │
                └──────┘   └──────┘   └──────┘
```

---

## 四、数据模型

### `vms` 表

| 字段 | 类型 | 说明 |
|------|------|------|
| id | TEXT (UUID) | 主键 |
| name | TEXT | VM 名称，如 `vm-001` |
| vcpu | INTEGER | vCPU 核数 |
| memory_gb | INTEGER | 内存 GiB |
| disk_gb | INTEGER | 磁盘 GiB |
| network_type | TEXT | `bridge` / `nat` |
| mac | TEXT | 虚拟网卡 MAC |
| vnc_port | INTEGER | VNC 端口 |
| ip | TEXT | VM IP（从 libvirt 获取，可为空） |
| status | TEXT | `creating` / `running` / `stopped` / `error` |
| created_at | DATETIME | 创建时间 |

### `tasks` 表

| 字段 | 类型 | 说明 |
|------|------|------|
| id | TEXT (UUID) | 主键 |
| type | TEXT | `create_vm` / `delete_vm` |
| ref_id | TEXT | 关联的 vm_id |
| status | TEXT | `pending` / `running` / `success` / `failed` |
| progress | INTEGER | 0-100 |
| message | TEXT | 当前步骤描述 |
| created_at | DATETIME | 创建时间 |

---

## 五、配置文件（config.yaml）

```yaml
host:
  bridge: br0
  image_dir: /var/lib/libvirt/images
  seed_image:
    # URL 或本地路径二选一
    source: "https://cloud-images.ubuntu.com/jammy/current/jammy-server-cloudimg-amd64.img"
    # source: "/data/images/jammy-server-cloudimg-amd64.img"
  ssh_key: /root/.ssh/id_ed25519

server:
  port: 8080

auth:
  username: admin
  password: admin
```

**种子镜像加载逻辑（启动时）：**

1. 判断 `source` 是否以 `http://` 或 `https://` 开头
2. **URL**：检查 `image_dir` 下是否已有同名文件，已有则跳过，否则下载
3. **本地路径**：验证文件存在，不存在则启动失败并报错

---

## 六、API 接口

```
# 认证
POST   /api/login
POST   /api/logout

# VM 管理
GET    /api/vms                    # 列表
POST   /api/vms                    # 创建（异步，返回 task_id）
GET    /api/vms/:id                # 详情
DELETE /api/vms/:id                # 删除（异步，返回 task_id）

# VM 操作
POST   /api/vms/:id/start          # 启动
POST   /api/vms/:id/stop           # 关机
POST   /api/vms/:id/restart        # 重启

# 任务状态
GET    /api/tasks/:task_id         # 查询异步任务进度

# 前端（embed）
GET    /*                          # 返回 index.html
```

**任务状态返回示例：**
```json
{
  "task_id": "xxx",
  "type": "create_vm",
  "status": "running",
  "progress": 60,
  "message": "正在等待 VM SSH 就绪...",
  "created_at": "2026-06-11T10:00:00Z"
}
```

---

## 七、目录结构

```
broodvm/
├── main.go
├── config.yaml
├── internal/
│   ├── api/
│   │   ├── handler/
│   │   │   ├── auth.go
│   │   │   ├── vm.go
│   │   │   └── task.go
│   │   ├── middleware/
│   │   │   └── auth.go
│   │   └── router.go
│   ├── service/
│   │   ├── vm.go
│   │   └── task.go
│   ├── store/
│   │   ├── db.go
│   │   ├── vm.go
│   │   └── task.go
│   └── libvirt/
│       └── client.go
├── templates/
│   └── vm-domain.xml.tmpl
└── web/
    ├── index.html
    ├── app.js
    └── style.css
```

---

## 八、前端页面

- **技术栈：** jQuery + Pico.css，CDN 引入，零构建
- **路由：** 单页，通过 hash 切换视图

| 页面 | 路径 | 内容 |
|------|------|------|
| 登录 | `#/login` | 用户名/密码表单 |
| VM 列表 | `#/` | 表格：名称/vCPU/内存/磁盘/IP/状态 + 操作按钮（启动/停止/重启/删除） |
| 创建 VM | `#/vms/new` | vCPU / 内存 / 磁盘 / 网络类型 + 创建进度条 |
| VM 详情 | `#/vms/:id` | 基本信息 + 状态 + 操作按钮 |

---

## 九、核心流程

### VM 创建流程

```
POST /api/vms { name, vcpu, memory_gb, disk_gb, network_type }
  1. 分配 MAC、VNC 端口（DB 中取最大值 +1）
  2. 渲染 vm-domain.xml.tmpl → 写磁盘
  3. 写 vms 记录（status=creating）+ 创建 task 记录
  4. 返回 202 + task_id
  （goroutine 异步）
  5. qemu-img create -f qcow2 -b 种子镜像 vm.qcow2 ${disk_gb}G
  6. 生成 cidata ISO（meta-data + user-data）
  7. virsh define vm.xml → virsh start vm
  8. 轮询等待 VM 启动（最长 3 分钟）
  9. 更新 VM status=running，task progress=100
```

### VM 删除流程

```
DELETE /api/vms/:id
  1. 创建 task 记录，返回 202 + task_id
  （goroutine 异步）
  2. virsh destroy vm（若运行中）
  3. virsh undefine vm
  4. 删除 qcow2 + cidata ISO + XML 文件
  5. 更新 VM status=deleted，task progress=100
```

### VM 启停流程

```
POST /api/vms/:id/start   → virsh start vm   → 更新 status=running
POST /api/vms/:id/stop    → virsh shutdown vm → 更新 status=stopped
POST /api/vms/:id/restart → virsh reboot vm  → 更新 status=running
（同步，直接返回结果）
```
