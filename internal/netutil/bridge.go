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
// 仅处理 IPv4，IPv6 地址被忽略。
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
