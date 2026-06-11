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
