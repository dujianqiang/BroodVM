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
