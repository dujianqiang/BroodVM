//go:build linux

package libvirt

import (
	"fmt"

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

func (c *LibvirtClient) GetIP(name string) (string, error) {
	dom, err := c.conn.LookupDomainByName(name)
	if err != nil {
		return "", err
	}
	defer dom.Free()
	ifaces, err := dom.ListAllInterfaceAddresses(golibvirt.DOMAIN_INTERFACE_ADDRESSES_SRC_LEASE)
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
