package libvirt

// MockClient 实现 Client 接口，用于单元测试。
type MockClient struct {
	RunningDomains map[string]bool
	DomainIPs      map[string]string

	DefineAndStartErr error
	DestroyErr        error
	UndefineErr       error
	StartErr          error
	ShutdownErr       error
	RebootErr         error
}

func NewMock() *MockClient {
	return &MockClient{
		RunningDomains: make(map[string]bool),
		DomainIPs:      make(map[string]string),
	}
}

func (m *MockClient) DefineAndStart(xmlDesc string) error {
	if m.DefineAndStartErr != nil {
		return m.DefineAndStartErr
	}
	return nil
}

func (m *MockClient) Destroy(name string) error {
	if m.DestroyErr != nil {
		return m.DestroyErr
	}
	delete(m.RunningDomains, name)
	return nil
}

func (m *MockClient) Undefine(name string) error { return m.UndefineErr }

func (m *MockClient) Start(name string) error {
	if m.StartErr != nil {
		return m.StartErr
	}
	m.RunningDomains[name] = true
	return nil
}

func (m *MockClient) Shutdown(name string) error {
	if m.ShutdownErr != nil {
		return m.ShutdownErr
	}
	delete(m.RunningDomains, name)
	return nil
}

func (m *MockClient) Reboot(name string) error { return m.RebootErr }

func (m *MockClient) IsRunning(name string) (bool, error) {
	return m.RunningDomains[name], nil
}

func (m *MockClient) GetIP(name, _ string) (string, error) {
	return m.DomainIPs[name], nil
}
