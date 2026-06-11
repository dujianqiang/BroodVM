# 桥接网络自动 IP 检测设计文档

**日期**：2026-06-11  
**背景**：IP 池功能要求用户手动填写网段、网关，用户需要了解宿主机网络配置，使用不便。  
**目标**：当 `ip_pool` 未配置时，自动从 `br0` 接口读取网段和网关，从子网中找出空闲 IP 分配给桥接 VM。

---

## 配置层

`ip_pool` 整块默认注释掉，保留注释说明用途。所有字段均为可选：

```yaml
# ip_pool 用于桥接模式 VM 的静态 IP 分配。
# 不配置时代码自动从 bridge 接口检测网段和网关。
#
# ip_pool:
#   gateway: 192.168.56.2      # 留空则从路由表自动检测
#   dns: 8.8.8.8,8.8.4.4      # 留空则默认 8.8.8.8
#   ips:                        # 留空则自动扫描子网分配
#     - 192.168.56.100/24
#     - 192.168.56.101/24
```

**优先级规则：**

| 字段 | 有配置 | 无配置 |
|------|--------|--------|
| `ips` | 沿用现有列表逻辑 | 自动扫描 `br0` 子网 |
| `gateway` | 用配置值 | 从 `/proc/net/route` 自动读取 |
| `dns` | 用配置值 | 默认 `8.8.8.8` |

---

## 自动检测逻辑

新增 `internal/netutil/bridge.go`，单一职责：读取网桥网络信息。

```go
// BridgeSubnet 返回指定接口的 IP 和所属子网。
func BridgeSubnet(ifaceName string) (ip net.IP, subnet *net.IPNet, err error)

// BridgeGateway 从 /proc/net/route 读取指定接口的默认网关。
func BridgeGateway(ifaceName string) (net.IP, error)
```

**`findFreePoolIP()` 改造逻辑：**

1. `cfg.Host.IPPool.IPs` 非空 → 沿用现有列表逻辑（不变）
2. `cfg.Host.IPPool.IPs` 为空 → 自动模式：
   - 调用 `BridgeSubnet(cfg.Host.Bridge)` 获取子网
   - 网关：优先用 `cfg.Host.IPPool.Gateway`，否则调用 `BridgeGateway`
   - 枚举子网内所有主机地址，排除：网段地址、广播地址、`br0` 自身 IP、网关 IP、DB 中非 deleted/error VM 已分配的 IP
   - 返回第一个空闲地址（附带子网掩码，格式为 CIDR）

---

## 错误处理

| 情况 | 行为 |
|------|------|
| `br0` 不存在或无 IP | 创建失败：`"无法读取网桥 br0 的网络信息"` |
| 路由表找不到网关且未配置 | 创建失败：`"无法自动检测网关，请在 ip_pool.gateway 中手动配置"` |
| 子网内所有地址被 DB 占用 | 创建失败：`"IP 池已耗尽"` |
| NAT 模式或手动指定 IP | 不触发自动检测，行为不变 |

---

## 涉及改动的文件

| 操作 | 文件 |
|------|------|
| 新增 | `internal/netutil/bridge.go` — `BridgeSubnet` / `BridgeGateway` |
| 新增 | `internal/netutil/bridge_test.go` — 单元测试 |
| 修改 | `internal/service/vm.go` — `findFreePoolIP()` 加入自动检测分支 |
| 修改 | `internal/service/vm_test.go` — 新增自动检测相关测试 |
| 修改 | `config.yaml` — `ip_pool` 改为注释形式 |
