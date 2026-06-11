# 桥接网络自动 IP 检测 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 当 `ip_pool.ips` 未配置时，自动从 `br0` 读取网段和网关，在子网中找出第一个未被 DB 占用的 IP，分配给桥接 VM。

**Architecture:** 新增 `internal/netutil/bridge.go` 提供 `BridgeSubnet`（读接口 IP/子网）和 `BridgeGateway`（解析 `/proc/net/route`）。`findFreePoolIP()` 签名改为返回 `(ip, gateway, dns string, err error)`：`ip_pool.ips` 非空时走列表逻辑，为空时走自动检测。`Create()` 移除 `if len(ip_pool.ips) > 0` 条件，统一调用 `findFreePoolIP()`，保证自动检测模式下 Gateway/DNS 也能正确写入 VM 记录。

**Tech Stack:** Go 标准库 `net`、`encoding/binary`、`os`（读 `/proc/net/route`）；模块路径 `github.com/dujianqiang/broodvm`。

---

## 文件变更清单

| 操作 | 文件 |
|------|------|
| 新增 | `internal/netutil/bridge.go` — `BridgeSubnet` / `ParseGateway` / `BridgeGateway` |
| 新增 | `internal/netutil/bridge_test.go` — netutil 单元测试 |
| 修改 | `internal/service/vm.go` — `findFreePoolIP()` 返回 `(ip, gateway, dns string, err error)`；`Create()` 移除 `len(ips)>0` 条件 |
| 修改 | `internal/service/vm_test.go` — 新增自动检测错误路径测试 |
| 修改 | `config.yaml` — `ip_pool` 改为注释形式 |

---

### Task 1: 新增 netutil/bridge.go

**Files:**
- Create: `internal/netutil/bridge.go`
- Create: `internal/netutil/bridge_test.go`

- [ ] **Step 1: 写失败测试**

新建 `internal/netutil/bridge_test.go`：

```go
package netutil_test

import (
	"net"
	"strings"
	"testing"

	"github.com/dujianqiang/broodvm/internal/netutil"
)

func TestBridgeSubnet_NotFound(t *testing.T) {
	_, _, err := netutil.BridgeSubnet("nonexistent-iface-xyz")
	if err == nil {
		t.Fatal("expected error for nonexistent interface")
	}
}

func TestBridgeSubnet_Loopback(t *testing.T) {
	ip, subnet, err := netutil.BridgeSubnet("lo")
	if err != nil {
		t.Skipf("loopback not available: %v", err)
	}
	if ip == nil || subnet == nil {
		t.Fatal("expected non-nil ip and subnet")
	}
	if !subnet.Contains(ip) {
		t.Errorf("subnet %v does not contain ip %v", subnet, ip)
	}
}

func TestParseGateway_Found(t *testing.T) {
	// 192.168.56.2 => bytes [C0 A8 38 02] => little-endian stored as hex "0238A8C0"
	data := "Iface\tDestination\tGateway\tFlags\tRefCnt\tUse\tMetric\tMask\tMTU\tWindow\tIRTT\n" +
		"br0\t00000000\t0238A8C0\t0003\t0\t0\t425\t00000000\t0\t0\t0\n"

	ip, err := netutil.ParseGateway(data, "br0")
	if err != nil {
		t.Fatalf("ParseGateway: %v", err)
	}
	want := net.ParseIP("192.168.56.2").To4()
	if !ip.Equal(want) {
		t.Errorf("gateway = %v, want %v", ip, want)
	}
}

func TestParseGateway_NotFound(t *testing.T) {
	data := "Iface\tDestination\tGateway\tFlags\tRefCnt\tUse\tMetric\tMask\tMTU\tWindow\tIRTT\n" +
		"eth0\t00000000\t0238A8C0\t0003\t0\t0\t425\t00000000\t0\t0\t0\n"

	_, err := netutil.ParseGateway(data, "br0")
	if err == nil {
		t.Fatal("expected error when interface not found")
	}
	if !strings.Contains(err.Error(), "br0") {
		t.Errorf("error should mention interface name, got: %v", err)
	}
}
```

- [ ] **Step 2: 运行测试，确认编译失败**

```bash
go test ./internal/netutil/... -v
```

期望：编译错误，`netutil` 包不存在。

- [ ] **Step 3: 实现 `internal/netutil/bridge.go`**

```go
package netutil

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"strings"
)

// BridgeSubnet 返回指定接口的 IPv4 地址和所属子网（网络地址）。
func BridgeSubnet(ifaceName string) (net.IP, *net.IPNet, error) {
	iface, err := net.InterfaceByName(ifaceName)
	if err != nil {
		return nil, nil, fmt.Errorf("interface %s not found: %w", ifaceName, err)
	}
	addrs, err := iface.Addrs()
	if err != nil {
		return nil, nil, fmt.Errorf("get addrs for %s: %w", ifaceName, err)
	}
	for _, addr := range addrs {
		if ipNet, ok := addr.(*net.IPNet); ok {
			if ip4 := ipNet.IP.To4(); ip4 != nil {
				network := &net.IPNet{
					IP:   ipNet.IP.Mask(ipNet.Mask),
					Mask: ipNet.Mask,
				}
				return ip4, network, nil
			}
		}
	}
	return nil, nil, fmt.Errorf("no IPv4 address on interface %s", ifaceName)
}

// BridgeGateway 从 /proc/net/route 读取指定接口的默认网关。
func BridgeGateway(ifaceName string) (net.IP, error) {
	data, err := os.ReadFile("/proc/net/route")
	if err != nil {
		return nil, fmt.Errorf("read /proc/net/route: %w", err)
	}
	return ParseGateway(string(data), ifaceName)
}

// ParseGateway 从 /proc/net/route 格式内容中解析指定接口的默认网关。
// 导出供测试使用。
func ParseGateway(data, ifaceName string) (net.IP, error) {
	for _, line := range strings.Split(data, "\n")[1:] {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		// 只匹配目标接口的默认路由（Destination == 00000000）
		if fields[0] != ifaceName || fields[1] != "00000000" {
			continue
		}
		gwBytes, err := hex.DecodeString(fields[2])
		if err != nil || len(gwBytes) != 4 {
			continue
		}
		// /proc/net/route 中网关以小端序 32 位整数存储
		gw := make(net.IP, 4)
		binary.BigEndian.PutUint32(gw, binary.LittleEndian.Uint32(gwBytes))
		return gw, nil
	}
	return nil, fmt.Errorf("no default gateway found for interface %s", ifaceName)
}
```

- [ ] **Step 4: 运行测试，确认通过**

```bash
go test ./internal/netutil/... -v
```

期望：4 个测试全部 `PASS`（`TestBridgeSubnet_Loopback` 在无 loopback 的环境下会 SKIP，属正常）。

- [ ] **Step 5: 提交**

```bash
git add internal/netutil/bridge.go internal/netutil/bridge_test.go
git commit -m "feat: add netutil.BridgeSubnet and BridgeGateway"
```

---

### Task 2: 重构 findFreePoolIP() 并加入自动检测分支

**Files:**
- Modify: `internal/service/vm.go`
- Modify: `internal/service/vm_test.go`

- [ ] **Step 1: 写失败测试**

在 `internal/service/vm_test.go` 顶部 import 中加入 `"strings"`，然后在文件末尾追加：

```go
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
```

- [ ] **Step 2: 运行测试，确认失败**

```bash
go test ./internal/service/... -run TestCreate_BridgeAutoDetect -v
```

期望：`FAIL` — `findFreePoolIP()` 当前无自动检测分支，`ip_pool.ips` 为空时返回"IP 池已耗尽"，错误信息不匹配。

- [ ] **Step 3: 修改 `internal/service/vm.go`**

**3a. 替换 import 块（加入 `encoding/binary`、`net` 和 netutil）：**

```go
import (
	"bytes"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"text/template"
	"time"

	"github.com/dujianqiang/broodvm/internal/cloudinit"
	"github.com/dujianqiang/broodvm/internal/config"
	lv "github.com/dujianqiang/broodvm/internal/libvirt"
	"github.com/dujianqiang/broodvm/internal/netutil"
	"github.com/dujianqiang/broodvm/internal/store"
	"github.com/google/uuid"
)
```

**3b. 将 `Create()` 中桥接 IP 分配的代码块（第 74-87 行）替换为：**

```go
	if req.NetworkType == "bridge" && req.IP == "" {
		ip, gw, dns, err := s.findFreePoolIP()
		if err != nil {
			s.mu.Unlock()
			return "", err
		}
		req.IP = ip
		req.Gateway = gw
		if req.DNS == "" {
			req.DNS = dns
		}
	}
```

**3c. 将 `findFreePoolIP()` 整体替换为：**

```go
// findFreePoolIP 在 s.mu 持有期间调用，返回可用的 IP（CIDR）、网关和 DNS。
// ip_pool.ips 非空时走列表逻辑；为空时自动从网桥接口检测网段并分配。
func (s *VMService) findFreePoolIP() (ip, gateway, dns string, err error) {
	vms, err := s.vmStore.List()
	if err != nil {
		return "", "", "", err
	}
	used := make(map[string]bool, len(vms))
	for _, vm := range vms {
		if vm.IP != "" && vm.Status != "error" {
			used[vm.IP] = true
		}
	}

	dns = s.cfg.Host.IPPool.DNS
	if dns == "" {
		dns = "8.8.8.8"
	}

	// 手动配置了 IP 列表 → 沿用列表逻辑
	if len(s.cfg.Host.IPPool.IPs) > 0 {
		for _, poolIP := range s.cfg.Host.IPPool.IPs {
			if !used[poolIP] {
				return poolIP, s.cfg.Host.IPPool.Gateway, dns, nil
			}
		}
		return "", "", "", fmt.Errorf("IP 池已耗尽，请扩充配置")
	}

	// 自动检测模式：从网桥接口读取网段
	bridgeName := s.cfg.Host.Bridge
	bridgeIP, subnet, err := netutil.BridgeSubnet(bridgeName)
	if err != nil {
		return "", "", "", fmt.Errorf("无法读取网桥 %s 的网络信息: %w", bridgeName, err)
	}

	var gwIP net.IP
	if s.cfg.Host.IPPool.Gateway != "" {
		gwIP = net.ParseIP(s.cfg.Host.IPPool.Gateway).To4()
		gateway = s.cfg.Host.IPPool.Gateway
	} else {
		gwIP, err = netutil.BridgeGateway(bridgeName)
		if err != nil {
			return "", "", "", fmt.Errorf("无法自动检测网关，请在 ip_pool.gateway 中手动配置: %w", err)
		}
		gateway = gwIP.String()
	}

	ones, bits := subnet.Mask.Size()
	total := 1 << uint(bits-ones)
	base := binary.BigEndian.Uint32(subnet.IP.To4())

	for i := 1; i < total-1; i++ {
		candidate := make(net.IP, 4)
		binary.BigEndian.PutUint32(candidate, base+uint32(i))
		if candidate.Equal(bridgeIP) || candidate.Equal(gwIP) {
			continue
		}
		cidr := fmt.Sprintf("%s/%d", candidate.String(), ones)
		if !used[cidr] {
			return cidr, gateway, dns, nil
		}
	}
	return "", "", "", fmt.Errorf("IP 池已耗尽，请扩充配置")
}
```

- [ ] **Step 4: 运行新增测试，确认通过**

```bash
go test ./internal/service/... -run TestCreate_BridgeAutoDetect -v
```

期望：`PASS`。

- [ ] **Step 5: 运行全量测试，确认无回归**

```bash
go test ./... -v 2>&1 | grep -E "^(ok|FAIL|---)"
```

期望：所有包 `ok`，无 `FAIL`。

- [ ] **Step 6: 提交**

```bash
git add internal/service/vm.go internal/service/vm_test.go
git commit -m "feat: auto-detect bridge subnet for IP allocation when ip_pool.ips unset"
```

---

### Task 3: 更新 config.yaml

**Files:**
- Modify: `config.yaml`

- [ ] **Step 1: 将 `config.yaml` 的 `ip_pool` 改为注释形式**

将整个 `config.yaml` 替换为：

```yaml
host:
  bridge: br0
  image_dir: /var/lib/libvirt/images
  seed_image:
    source: "https://cloud-images.ubuntu.com/jammy/current/jammy-server-cloudimg-amd64.img"
  ssh_key: /root/.ssh/id_ed25519
  # ip_pool 用于桥接模式 VM 的静态 IP 分配。
  # 不配置时代码自动从 bridge 接口检测网段和网关。
  #
  # ip_pool:
  #   gateway: 192.168.56.2      # 留空则从路由表自动检测
  #   dns: 8.8.8.8,8.8.4.4      # 留空则默认 8.8.8.8
  #   ips:                        # 留空则自动扫描子网分配
  #     - 192.168.56.100/24
  #     - 192.168.56.101/24

server:
  port: 8080

auth:
  username: admin
  password: admin
```

- [ ] **Step 2: 提交**

```bash
git add config.yaml
git commit -m "chore: comment out ip_pool config, auto-detection is now default"
```
