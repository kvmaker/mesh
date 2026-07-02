package config

import (
	"fmt"
	"os"

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
}

func Default() *Config {
	return &Config{
		Domain:     "localhost",
		ListenAddr: ":443",
		Network:    "10.100.0.0/24",
		DataDir:    "/etc/mesh",
		CertDir:    "/etc/mesh/certs",
		TunName:    "mesh0",
		TunMTU:     1300,
	}
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
