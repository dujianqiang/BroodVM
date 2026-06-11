package store

import (
	"time"
)

type VM struct {
	ID          string
	Name        string
	VCPU        int
	MemoryGB    int
	DiskGB      int
	NetworkType string
	MAC         string
	VNCPort     int
	IP          string
	Status      string
	CreatedAt   time.Time
}

type VMStore struct{ db *DB }

func NewVMStore(db *DB) *VMStore { return &VMStore{db: db} }

func (s *VMStore) Create(vm *VM) error {
	_, err := s.db.Exec(
		`INSERT INTO vms (id,name,vcpu,memory_gb,disk_gb,network_type,mac,vnc_port,ip,status,created_at)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		vm.ID, vm.Name, vm.VCPU, vm.MemoryGB, vm.DiskGB,
		vm.NetworkType, vm.MAC, vm.VNCPort, vm.IP, vm.Status,
		vm.CreatedAt.Format(time.RFC3339),
	)
	return err
}

func (s *VMStore) Get(id string) (*VM, error) {
	row := s.db.QueryRow(
		`SELECT id,name,vcpu,memory_gb,disk_gb,network_type,mac,vnc_port,ip,status,created_at
		 FROM vms WHERE id=?`, id)
	return scanVM(row)
}

func (s *VMStore) List() ([]*VM, error) {
	rows, err := s.db.Query(
		`SELECT id,name,vcpu,memory_gb,disk_gb,network_type,mac,vnc_port,ip,status,created_at
		 FROM vms WHERE status != 'deleted' ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var vms []*VM
	for rows.Next() {
		vm, err := scanVM(rows)
		if err != nil {
			return nil, err
		}
		vms = append(vms, vm)
	}
	return vms, rows.Err()
}

func (s *VMStore) UpdateStatus(id, status, ip string) error {
	_, err := s.db.Exec(`UPDATE vms SET status=?, ip=? WHERE id=?`, status, ip, id)
	return err
}

func (s *VMStore) Delete(id string) error {
	_, err := s.db.Exec(`UPDATE vms SET status='deleted' WHERE id=?`, id)
	return err
}

// NextVNCPort 返回下一个可用 VNC 端口（从 5900 开始）。
func (s *VMStore) NextVNCPort() (int, error) {
	row := s.db.QueryRow(
		`SELECT COALESCE(MAX(vnc_port), 5899) FROM vms WHERE status != 'deleted'`)
	var maxPort int
	if err := row.Scan(&maxPort); err != nil {
		return 0, err
	}
	return maxPort + 1, nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanVM(s rowScanner) (*VM, error) {
	var vm VM
	var createdAt string
	err := s.Scan(
		&vm.ID, &vm.Name, &vm.VCPU, &vm.MemoryGB, &vm.DiskGB,
		&vm.NetworkType, &vm.MAC, &vm.VNCPort, &vm.IP, &vm.Status, &createdAt,
	)
	if err != nil {
		return nil, err
	}
	vm.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	return &vm, nil
}
