# BroodVM 设计文档

**日期：** 2026-06-11  
**项目：** BroodVM — KVM+QEMU 多实例虚拟化后台管理系统

---

## 一、项目背景

基于 KVM/QEMU 的单宿主机虚拟机管理系统，提供 Web 后台界面，支持 VM 全生命周期管理（创建/启停/删除）和软件安装（通过 SSH 执行 shell 脚本）。

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
| SSH | golang.org/x/crypto/ssh | 连接 VM 执行安装脚本 |

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

## 四、VM 标准（规格模板）

### 4.1 设计思路

VM 标准分两层：

1. **命名模板**：预定义多套规格（如"默认"、"高性能"），每套包含标准参数 + 高级参数
2. **创建时覆盖**：创建 VM 时先选模板，再按需修改任意参数

系统内置一套"默认模板"，首次启动时自动写入 DB。

### 4.2 参数分层

**标准参数（表单直接展示）：**

| 参数 | 类型 | 说明 |
|------|------|------|
| vcpu | INTEGER | vCPU 核数 |
| memory_gb | INTEGER | 内存 GiB |
| disk_gb | INTEGER | 磁盘 GiB |
| network_type | TEXT | `bridge` / `nat`，宿主机不支持则置灰 |

**高级参数（点"高级"展开，结构化表单）：**

| 参数 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| cpu_mode | TEXT | `host-passthrough` | CPU 透传模式，宿主机不支持的选项置灰 |
| cpu_sockets | INTEGER | 1 | CPU 拓扑：插槽数 |
| cpu_cores | INTEGER | vcpu | CPU 拓扑：每插槽核数 |
| cpu_threads | INTEGER | 1 | CPU 拓扑：每核线程数 |
| memory_balloon | BOOLEAN | false | 是否启用内存气球动态调整 |
| hugepages | BOOLEAN | false | 是否使用大页内存，宿主机未配置则置灰 |
| disk_cache | TEXT | `none` | 磁盘缓存模式：`none` / `writeback` |
| disk_io | TEXT | `native` | 磁盘 IO 模式：`native` / `threads` |

### 4.3 宿主机能力检测

启动时探测一次，结果缓存在内存，通过 `GET /api/host/capabilities` 供前端获取：

```json
{
  "network": {
    "bridge": true,   // 检测 br0 是否存在
    "nat": true       // libvirt default network 是否活跃
  },
  "cpu_modes": ["host-passthrough", "host-model", "custom"],
  "hugepages": false  // /sys/kernel/mm/hugepages 是否配置
}
```

前端根据此响应对不支持的选项添加 `disabled` 属性并置灰展示。

### 4.4 默认模板内容

```json
{
  "name": "默认",
  "vcpu": 2,
  "memory_gb": 4,
  "disk_gb": 20,
  "network_type": "bridge",
  "cpu_mode": "host-passthrough",
  "cpu_sockets": 1,
  "cpu_cores": 2,
  "cpu_threads": 1,
  "memory_balloon": false,
  "hugepages": false,
  "disk_cache": "none",
  "disk_io": "native"
}
```

---

## 五、数据模型

### `vm_standards` 表（规格模板）

| 字段 | 类型 | 说明 |
|------|------|------|
| id | TEXT (UUID) | 主键 |
| name | TEXT | 模板名称，如"默认"、"高性能" |
| is_default | BOOLEAN | 是否为默认模板 |
| vcpu | INTEGER | vCPU 核数 |
| memory_gb | INTEGER | 内存 GiB |
| disk_gb | INTEGER | 磁盘 GiB |
| network_type | TEXT | `bridge` / `nat` |
| cpu_mode | TEXT | CPU 模式 |
| cpu_sockets | INTEGER | CPU 插槽数 |
| cpu_cores | INTEGER | 每插槽核数 |
| cpu_threads | INTEGER | 每核线程数 |
| memory_balloon | BOOLEAN | 是否启用 balloon |
| hugepages | BOOLEAN | 是否启用大页 |
| disk_cache | TEXT | 磁盘缓存模式 |
| disk_io | TEXT | 磁盘 IO 模式 |
| created_at | DATETIME | 创建时间 |

### `vms` 表

| 字段 | 类型 | 说明 |
|------|------|------|
| id | TEXT (UUID) | 主键 |
| name | TEXT | VM 名称，如 `vm-001` |
| standard_id | TEXT | 来源模板 ID（仅记录，创建后独立存储参数） |
| vcpu | INTEGER | vCPU 核数（创建时从模板复制，可覆盖） |
| memory_gb | INTEGER | 内存 GiB |
| disk_gb | INTEGER | 磁盘 GiB |
| network_type | TEXT | `bridge` / `nat` |
| cpu_mode | TEXT | CPU 模式 |
| cpu_sockets | INTEGER | CPU 插槽数 |
| cpu_cores | INTEGER | 每插槽核数 |
| cpu_threads | INTEGER | 每核线程数 |
| memory_balloon | BOOLEAN | 是否启用 balloon |
| hugepages | BOOLEAN | 是否启用大页 |
| disk_cache | TEXT | 磁盘缓存模式 |
| disk_io | TEXT | 磁盘 IO 模式 |
| mac | TEXT | 虚拟网卡 MAC |
| vnc_port | INTEGER | VNC 端口 |
| ip | TEXT | VM IP（从 libvirt 获取，可为空） |
| status | TEXT | `creating` / `running` / `stopped` / `error` |
| xml_path | TEXT | 磁盘上 XML 文件路径 |
| xml_definition | TEXT | XML 内容（供前端展示和编辑） |
| created_at | DATETIME | 创建时间 |

### `software_installs` 表

| 字段 | 类型 | 说明 |
|------|------|------|
| id | TEXT (UUID) | 主键 |
| vm_id | TEXT | 关联 VM |
| software | TEXT | 软件名，如 `openclaw` |
| status | TEXT | `pending` / `running` / `success` / `failed` |
| log | TEXT | 安装脚本输出 |
| created_at | DATETIME | 创建时间 |

### `tasks` 表

| 字段 | 类型 | 说明 |
|------|------|------|
| id | TEXT (UUID) | 主键 |
| type | TEXT | `create_vm` / `install_software` |
| ref_id | TEXT | 关联的 vm_id |
| status | TEXT | `pending` / `running` / `success` / `failed` |
| progress | INTEGER | 0-100 |
| message | TEXT | 当前步骤描述 |
| created_at | DATETIME | 创建时间 |

---

## 六、配置文件（config.yaml）

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

管理页面"系统设置"可在线修改 `source`，修改后重新触发上述逻辑。

---

## 七、API 接口

```
# 认证
POST   /api/login
POST   /api/logout

# VM 管理
GET    /api/vms                    # 列表
POST   /api/vms                    # 创建（异步，返回 task_id）
GET    /api/vms/:id                # 详情
DELETE /api/vms/:id                # 删除

# VM 操作
POST   /api/vms/:id/start          # 启动
POST   /api/vms/:id/stop           # 关机
POST   /api/vms/:id/restart        # 重启

# XML 配置
GET    /api/vms/:id/xml            # 获取 XML
PUT    /api/vms/:id/xml            # 更新并重新应用 XML

# 软件安装
POST   /api/vms/:id/software       # 安装软件（异步，返回 task_id）
GET    /api/vms/:id/software       # 安装记录列表

# 任务状态
GET    /api/tasks/:task_id         # 查询异步任务进度

# VM 标准模板
GET    /api/standards              # 列表
POST   /api/standards              # 创建模板
GET    /api/standards/:id          # 详情
PUT    /api/standards/:id          # 更新模板
DELETE /api/standards/:id          # 删除模板

# 宿主机能力
GET    /api/host/capabilities      # 返回宿主机支持的网络/CPU/hugepages 能力

# 系统设置
GET    /api/settings               # 获取当前配置（含种子镜像来源）
PUT    /api/settings               # 更新配置（如修改 seed_image.source）

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

## 八、目录结构

```
broodvm/
├── main.go
├── config.yaml
├── cmd/
│   └── server.go
├── internal/
│   ├── api/
│   │   ├── handler/
│   │   │   ├── auth.go
│   │   │   ├── vm.go
│   │   │   ├── xml.go
│   │   │   ├── software.go
│   │   │   ├── standard.go
│   │   │   ├── host.go
│   │   │   └── task.go
│   │   ├── middleware/
│   │   │   └── auth.go
│   │   └── router.go
│   ├── service/
│   │   ├── vm.go
│   │   ├── standard.go         # 规格模板 CRUD + 默认模板初始化
│   │   ├── xml.go
│   │   ├── ssh.go
│   │   └── task.go
│   ├── store/
│   │   ├── db.go
│   │   ├── vm.go
│   │   ├── standard.go
│   │   └── task.go
│   ├── libvirt/
│   │   └── client.go
│   └── capability/
│       └── detector.go         # 宿主机能力检测（启动时执行一次）
├── scripts/
│   └── install_openclaw.sh
├── templates/
│   └── vm-domain.xml.tmpl
└── web/
    ├── index.html
    ├── app.js
    └── style.css
```

---

## 九、前端页面

- **技术栈：** jQuery + Pico.css，CDN 引入，零构建
- **路由：** 单页，通过 hash 切换视图

| 页面 | 路径 | 内容 |
|------|------|------|
| 登录 | `#/login` | 用户名/密码表单 |
| VM 列表 | `#/` | 表格：名称/vCPU/内存/磁盘/IP/状态 + 操作按钮 |
| 创建 VM | `#/vms/new` | 选模板 → 标准参数表单 → 高级参数（折叠）→ 创建进度条 |
| VM 详情 | `#/vms/:id` | 基本信息 + XML 编辑器 + 软件安装 + 安装历史 |
| 标准模板 | `#/standards` | 模板列表 + 新建/编辑/删除 |
| 系统设置 | `#/settings` | 种子镜像来源（URL/本地路径）+ 其他宿主机配置 |

**创建 VM 表单交互：**
1. 顶部下拉选择模板（默认选"默认"），选后自动填充所有参数
2. 标准参数区：vCPU / 内存 / 磁盘 / 网络类型（不支持的选项 `disabled` 置灰）
3. "高级选项 ▼" 折叠区：CPU mode / topology / balloon / hugepages / disk cache / IO（不支持的置灰）
4. 提交后展示进度条，轮询 task 接口更新

---

## 十、核心流程

### VM 创建流程

```
POST /api/vms
  1. 分配 MAC、VNC 端口（DB 中取最大值 +1）
  2. 渲染 vm-domain.xml.tmpl → 写磁盘 + 存 DB
  3. 创建 task 记录，返回 202 + task_id
  （goroutine 异步）
  4. qemu-img create -f qcow2 -b 种子镜像 vm.qcow2 ${disk_gb}G
  5. 生成 cidata ISO（meta-data + user-data + network-config）
  6. virsh define vm.xml → virsh start vm
  7. 轮询等待 VM SSH 就绪（最长 3 分钟）
  8. 更新 VM status=running，task progress=100
```

### 软件安装流程

```
POST /api/vms/:id/software { "software": "openclaw" }
  1. 创建 task + software_installs 记录，返回 202 + task_id
  （goroutine 异步）
  2. SSH 连接 VM → 执行 scripts/install_openclaw.sh
  3. 实时写入 log 字段，更新 task progress
  4. 脚本退出码 0 → success，否则 failed
```

### XML 更新流程

```
PUT /api/vms/:id/xml { "xml": "..." }
  1. 验证 XML 合法性
  2. 更新 DB xml_definition 字段
  3. 覆写磁盘 XML 文件
  4. virsh define vm.xml 重新应用
```
