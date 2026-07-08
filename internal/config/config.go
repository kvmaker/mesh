package config

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Domain     string `yaml:"domain"`
	ListenAddr string `yaml:"listen_addr"`
	Network    string `yaml:"network"`
	DataDir    string `yaml:"data_dir"`
	CertDir    string `yaml:"cert_dir"`
	TunName    string `yaml:"tun_name"`
	TunMTU     int    `yaml:"tun_mtu"`

	// TLSTestMode 在 e2e 测试场景启用自签证书，由 MESH_TEST_TLS 环境变量控制。
	// yaml tag "-" 表示该字段不入配置文件，仅供测试开关使用。
	TLSTestMode bool `yaml:"-"`
}

// applyTestMode 根据环境变量 MESH_TEST_TLS 决定是否启用测试模式（自签证书）。
// 生产环境不设此变量，行为不受影响。
func (c *Config) applyTestMode() {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("MESH_TEST_TLS")))
	switch v {
	case "off", "1", "true", "on", "self":
		c.TLSTestMode = true
	}
}

func Default() *Config {
	cfg := &Config{
		Domain:     "localhost",
		ListenAddr: ":443",
		Network:    "10.100.0.0/24",
		DataDir:    "/etc/mesh",
		CertDir:    "/etc/mesh/certs",
		TunName:    "mesh0",
		TunMTU:     1300,
	}
	// 在这里调用，确保所有获取 Config 的路径（Default 直用、Load 成功、Load 失败回退）
	// 都能读取 MESH_TEST_TLS 环境变量，e2e 测试模式下不会误走 Let's Encrypt。
	cfg.applyTestMode()
	return cfg
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	cfg := Default()
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	return cfg, nil
}
