package config

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Host struct {
		Bridge    string `yaml:"bridge"`
		ImageDir  string `yaml:"image_dir"`
		SeedImage struct {
			Source string `yaml:"source"`
		} `yaml:"seed_image"`
		SSHKey string `yaml:"ssh_key"`
		IPPool struct {
			Gateway string   `yaml:"gateway"`
			DNS     string   `yaml:"dns"`
			IPs     []string `yaml:"ips"`
		} `yaml:"ip_pool"`
	} `yaml:"host"`
	Server struct {
		Port int `yaml:"port"`
	} `yaml:"server"`
	Auth struct {
		Username string `yaml:"username"`
		Password string `yaml:"password"`
	} `yaml:"auth"`
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	return &cfg, nil
}

// EnsureSeedImage 确保种子镜像在本地存在，返回本地路径。
// URL 来源：若 image_dir 下已有同名文件则跳过，否则下载。
// 本地路径：验证文件存在，不存在则返回错误。
func EnsureSeedImage(cfg *Config) (string, error) {
	src := cfg.Host.SeedImage.Source
	imageDir := cfg.Host.ImageDir

	if strings.HasPrefix(src, "http://") || strings.HasPrefix(src, "https://") {
		filename := filepath.Base(src)
		localPath := filepath.Join(imageDir, filename)
		if _, err := os.Stat(localPath); err == nil {
			return localPath, nil
		}
		if err := downloadFile(localPath, src); err != nil {
			return "", fmt.Errorf("download seed image: %w", err)
		}
		return localPath, nil
	}

	if _, err := os.Stat(src); err != nil {
		return "", fmt.Errorf("seed image not found at %s", src)
	}
	return src, nil
}

func downloadFile(dest, url string) error {
	resp, err := http.Get(url) //nolint:gosec
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, url)
	}

	if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
		return err
	}

	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = io.Copy(f, resp.Body)
	if err != nil {
		f.Close()
		os.Remove(dest)
		return err
	}
	return nil
}
