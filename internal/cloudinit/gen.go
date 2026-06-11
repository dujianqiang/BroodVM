package cloudinit

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Generate 在 destDir 下生成 <vmName>-cidata.iso，返回 ISO 路径。
func Generate(destDir, vmName, sshPubKey string) (string, error) {
	tmpDir, err := os.MkdirTemp("", "cidata-"+vmName+"-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmpDir)

	metaData := fmt.Sprintf("instance-id: %s\nlocal-hostname: %s\n", vmName, vmName)
	userData := fmt.Sprintf(`#cloud-config
users:
  - name: ubuntu
    sudo: ALL=(ALL) NOPASSWD:ALL
    shell: /bin/bash
    lock_passwd: true
    ssh_authorized_keys:
      - %s
`, strings.TrimSpace(sshPubKey))

	if err := os.WriteFile(filepath.Join(tmpDir, "meta-data"), []byte(metaData), 0644); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "user-data"), []byte(userData), 0644); err != nil {
		return "", err
	}

	isoPath := filepath.Join(destDir, vmName+"-cidata.iso")
	out, err := exec.Command("genisoimage",
		"-output", isoPath,
		"-volid", "cidata",
		"-joliet", "-rock",
		filepath.Join(tmpDir, "meta-data"),
		filepath.Join(tmpDir, "user-data"),
	).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("genisoimage failed: %w\n%s", err, out)
	}
	return isoPath, nil
}
