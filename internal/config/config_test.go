package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/dujianqiang/broodvm/internal/config"
)

func TestLoad(t *testing.T) {
	yaml := `
host:
  bridge: br0
  image_dir: /tmp/images
  seed_image:
    source: /tmp/seed.img
  ssh_key: /root/.ssh/id_ed25519
server:
  port: 9090
auth:
  username: admin
  password: secret
`
	f := filepath.Join(t.TempDir(), "config.yaml")
	os.WriteFile(f, []byte(yaml), 0644)

	cfg, err := config.Load(f)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Host.Bridge != "br0" {
		t.Errorf("bridge = %q, want br0", cfg.Host.Bridge)
	}
	if cfg.Server.Port != 9090 {
		t.Errorf("port = %d, want 9090", cfg.Server.Port)
	}
	if cfg.Auth.Password != "secret" {
		t.Errorf("password = %q, want secret", cfg.Auth.Password)
	}
}

func TestEnsureSeedImage_LocalMissing(t *testing.T) {
	cfg := &config.Config{}
	cfg.Host.SeedImage.Source = "/nonexistent/seed.img"
	cfg.Host.ImageDir = t.TempDir()

	_, err := config.EnsureSeedImage(cfg)
	if err == nil {
		t.Fatal("expected error for missing local file")
	}
}

func TestEnsureSeedImage_LocalExists(t *testing.T) {
	dir := t.TempDir()
	seedPath := filepath.Join(dir, "seed.img")
	os.WriteFile(seedPath, []byte("fake"), 0644)

	cfg := &config.Config{}
	cfg.Host.SeedImage.Source = seedPath
	cfg.Host.ImageDir = dir

	got, err := config.EnsureSeedImage(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != seedPath {
		t.Errorf("got %q, want %q", got, seedPath)
	}
}

func TestLoad_IPPool(t *testing.T) {
	yaml := `
host:
  bridge: br0
  ip_pool:
    gateway: 192.168.1.1
    dns: 8.8.8.8,8.8.4.4
    ips:
      - 192.168.1.100/24
      - 192.168.1.101/24
server:
  port: 8080
auth:
  username: admin
  password: admin
`
	f := filepath.Join(t.TempDir(), "config.yaml")
	os.WriteFile(f, []byte(yaml), 0644)

	cfg, err := config.Load(f)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Host.IPPool.Gateway != "192.168.1.1" {
		t.Errorf("gateway = %q, want 192.168.1.1", cfg.Host.IPPool.Gateway)
	}
	if cfg.Host.IPPool.DNS != "8.8.8.8,8.8.4.4" {
		t.Errorf("dns = %q, want 8.8.8.8,8.8.4.4", cfg.Host.IPPool.DNS)
	}
	if len(cfg.Host.IPPool.IPs) != 2 {
		t.Errorf("ips len = %d, want 2", len(cfg.Host.IPPool.IPs))
	}
	if cfg.Host.IPPool.IPs[0] != "192.168.1.100/24" {
		t.Errorf("ips[0] = %q, want 192.168.1.100/24", cfg.Host.IPPool.IPs[0])
	}
}
