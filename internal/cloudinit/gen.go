package cloudinit

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type NetworkConfig struct {
	IP      string // CIDR 格式，如 192.168.1.100/24
	Gateway string
	DNS     string // 逗号分隔，如 8.8.8.8,8.8.4.4
	MAC     string
}

// Generate 在 destDir 下生成 <vmName>-cidata.iso，返回 ISO 路径。
// net 为 nil 时使用 DHCP，否则写入静态 IP 配置。
func Generate(destDir, vmName, sshPubKey string, net *NetworkConfig) (string, error) {
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

	files := map[string]string{
		"meta-data": metaData,
		"user-data": userData,
	}

	if net != nil && net.IP != "" {
		files["network-config"] = buildNetworkConfig(net)
	}

	for name, content := range files {
		if err := os.WriteFile(filepath.Join(tmpDir, name), []byte(content), 0644); err != nil {
			return "", err
		}
	}

	// genisoimage 参数：按文件名顺序传入，保证 meta-data 和 user-data 必须存在
	args := []string{
		"-output", filepath.Join(destDir, vmName+"-cidata.iso"),
		"-volid", "cidata",
		"-joliet", "-rock",
		filepath.Join(tmpDir, "meta-data"),
		filepath.Join(tmpDir, "user-data"),
	}
	if _, ok := files["network-config"]; ok {
		args = append(args, filepath.Join(tmpDir, "network-config"))
	}

	out, err := exec.Command("genisoimage", args...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("genisoimage failed: %w\n%s", err, out)
	}
	return filepath.Join(destDir, vmName+"-cidata.iso"), nil
}

func buildNetworkConfig(net *NetworkConfig) string {
	dns := net.DNS
	if dns == "" {
		dns = "8.8.8.8"
	}
	// 转成 YAML 列表
	dnsEntries := ""
	for _, d := range strings.Split(dns, ",") {
		d = strings.TrimSpace(d)
		if d != "" {
			dnsEntries += fmt.Sprintf("        - %s\n", d)
		}
	}

	matchLine := ""
	if net.MAC != "" {
		matchLine = fmt.Sprintf(`    match:
      macaddress: "%s"
`, net.MAC)
	}

	return fmt.Sprintf(`version: 2
ethernets:
  eth0:
%s    addresses:
      - %s
    gateway4: %s
    nameservers:
      addresses:
%s`, matchLine, net.IP, net.Gateway, dnsEntries)
}
