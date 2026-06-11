//go:build linux

package libvirt

import (
	"fmt"
	"os"
	"strings"

	golibvirt "libvirt.org/go/libvirt"
)

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

func (c *LibvirtClient) GetIP(name, mac string) (string, error) {
	dom, err := c.conn.LookupDomainByName(name)
	if err != nil {
		return "", err
	}
	defer dom.Free()

	// 1. dnsmasq lease（NAT 模式）
	if ip := ipFromLibvirt(dom, golibvirt.DOMAIN_INTERFACE_ADDRESSES_SRC_LEASE); ip != "" {
		return ip, nil
	}
	// 2. qemu-guest-agent（桥接模式，需 VM 内安装 guest agent）
	if ip := ipFromLibvirt(dom, golibvirt.DOMAIN_INTERFACE_ADDRESSES_SRC_AGENT); ip != "" {
		return ip, nil
	}
	// 3. 宿主机 ARP 表（桥接模式通用 fallback，通过 MAC 匹配）
	if mac != "" {
		if ip := ipFromARP(mac); ip != "" {
			return ip, nil
		}
	}
	return "", nil
}

func ipFromLibvirt(dom interface {
	ListAllInterfaceAddresses(golibvirt.DomainInterfaceAddressesSource) ([]golibvirt.DomainInterface, error)
}, src golibvirt.DomainInterfaceAddressesSource) string {
	ifaces, err := dom.ListAllInterfaceAddresses(src)
	if err != nil {
		return ""
	}
	for _, iface := range ifaces {
		for _, addr := range iface.Addrs {
			if addr.Type == golibvirt.IP_ADDR_TYPE_IPV4 && addr.Addr != "127.0.0.1" {
				return addr.Addr
			}
		}
	}
	return ""
}

// ipFromARP 在 /proc/net/arp 中按 MAC 地址查找 IP（桥接模式无 DHCP lease 时使用）。
func ipFromARP(mac string) string {
	data, err := os.ReadFile("/proc/net/arp")
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n")[1:] {
		fields := strings.Fields(line)
		if len(fields) >= 4 && strings.EqualFold(fields[3], mac) {
			return fields[0]
		}
	}
	return ""
}
