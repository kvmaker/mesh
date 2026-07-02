package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Endpoint   string `yaml:"endpoint"`
	ListenPort int    `yaml:"listen_port"`
	APIPort    int    `yaml:"api_port"`
	Network    string `yaml:"network"`
	DataDir    string `yaml:"data_dir"`
}

func Default() *Config {
	return &Config{
		Endpoint:   "0.0.0.0:51820",
		ListenPort: 51820,
		APIPort:    8080,
		Network:    "10.100.0.0/24",
		DataDir:    "/etc/mesh",
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
