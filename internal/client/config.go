package client

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// ClientConfig 保存客户端加入 mesh 网络所需的全部信息。
type ClientConfig struct {
	ServerDomain string `json:"server_domain"`
	DeviceSecret string `json:"device_secret"`
	DeviceIP     string `json:"device_ip"`
	DeviceID     string `json:"device_id"`
	NetworkCIDR  string `json:"network_cidr"`
	InsecureTLS  bool   `json:"insecure_tls,omitempty"`
}

// ConfigDir 返回客户端配置目录（~/.mesh）。
func ConfigDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".mesh")
}

// LoadClientConfig 从磁盘读取并解析配置文件。
func LoadClientConfig() (*ClientConfig, error) {
	data, err := os.ReadFile(filepath.Join(ConfigDir(), "config.json"))
	if err != nil {
		return nil, err
	}
	var cfg ClientConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	return &cfg, nil
}

// SaveClientConfig 将配置序列化后写入磁盘，权限 0600。
func SaveClientConfig(cfg *ClientConfig) error {
	if err := os.MkdirAll(ConfigDir(), 0700); err != nil {
		return err
	}
	data, _ := json.MarshalIndent(cfg, "", "  ")
	return os.WriteFile(filepath.Join(ConfigDir(), "config.json"), data, 0600)
}
