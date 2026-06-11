package libvirt

// Client 定义 BroodVM 所需的 libvirt 操作。
type Client interface {
	DefineAndStart(xmlDesc string) error
	Destroy(name string) error
	Undefine(name string) error
	Start(name string) error
	Shutdown(name string) error
	Reboot(name string) error
	IsRunning(name string) (bool, error)
	GetIP(name string) (string, error)
}
