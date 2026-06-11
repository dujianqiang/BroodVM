# IP 池设计文档

**日期**：2026-06-11  
**问题背景**：桥接模式（bridge）VM 依赖 ARP 表发现 IP，VM 刚启动时可能尚未出现在 ARP 表中，导致列表页 IP 显示为空。  
**解决思路**：引入 IP 池，桥接 VM 创建时从池中自动分配静态 IP，通过 cloud-init 写入 network-config，彻底绕开运行时 IP 发现问题。

---

## 配置

在 `config.yaml` 的 `host` 节下新增 `ip_pool`：

```yaml
host:
  bridge: br0
  image_dir: /var/lib/libvirt/images
  ip_pool:
    gateway: 192.168.1.1
    dns: 8.8.8.8,8.8.4.4
    ips:
      - 192.168.1.100/24
      - 192.168.1.101/24
      - 192.168.1.102/24
```

- IP 使用 CIDR 格式（含子网掩码），可直接传给现有的 `buildNetworkConfig()`
- `gateway` 和 `dns` 为池内所有 IP 共用
- `ip_pool` 为空时行为与现在相同（桥接 VM 使用 DHCP）

## 分配逻辑

`VMService` 新增 `allocateIP() (string, error)` 方法，加 `sync.Mutex` 保护：

1. 读取 `config.ip_pool.ips` 列表
2. 查询 DB 中 `status != "deleted"` 且 IP 非空的所有 VM，构建已用 IP 集合
3. 遍历池子，返回第一个不在已用集合中的 IP
4. 池子耗尽返回错误

**触发条件**（在 `Create()` 中）：
- `NetworkType == "bridge"` 且用户未手动指定 `IP` 时，自动调用 `allocateIP()`
- 用户手动指定 IP 时，沿用现有行为，不触发

## 释放逻辑

VM 删除时 status 改为 `"deleted"`，无需额外操作。`allocateIP()` 查询时自动跳过 deleted 状态的 VM，其 IP 自然归还到可用集合。

## 错误处理与边界情况

| 情况 | 行为 |
|------|------|
| `ip_pool` 未配置 | 桥接 VM 继续使用 DHCP，与现在相同 |
| 池子耗尽 | `Create()` 立即报错 `"IP 池已耗尽，请扩充配置"`，不创建 VM 记录 |
| config 删掉已分配的 IP | 已运行的 VM 不受影响；仅影响下次分配 |
| 并发创建 | `sync.Mutex` 保护 `allocateIP()`，同一 IP 不会重复分配 |

## 涉及改动的文件

| 文件 | 改动 |
|------|------|
| `config.yaml` | 新增 `ip_pool` 配置示例 |
| `internal/config/config.go` | `Config.Host` 增加 `IPPool` 字段 |
| `internal/service/vm.go` | 新增 `allocateIP()`，`Create()` 中桥接模式自动调用 |
