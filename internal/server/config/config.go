package config

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// Mode 取值。full = 创建 TUN、分配 server IP(server 作为 VPN 节点);
// relay = 纯中继,不创建 TUN、不分配 server IP,仅做客户端间转发。
// TLSMode 取值。autocert = 自动证书(Let's Encrypt,默认);
// none = 不启用 TLS(仅在已由外部 TLS 终结的部署场景下使用)。
const (
	ModeFull    = "full"
	ModeRelay   = "relay"
	TLSAutocert = "autocert"
	TLSNone     = "none"
)

type Config struct {
	Domain     string `yaml:"domain"`
	ListenAddr string `yaml:"listen_addr"`
	Network    string `yaml:"network"`
	DataDir    string `yaml:"data_dir"`
	CertDir    string `yaml:"cert_dir"`
	TunName    string `yaml:"tun_name"`
	TunMTU     int    `yaml:"tun_mtu"`
	Mode       string `yaml:"mode"`
	TLSMode    string `yaml:"tls_mode"`

	// TLSTestMode 在 e2e 测试场景启用自签证书，由 MESH_TEST_TLS 环境变量控制。
	// yaml tag "-" 表示该字段不入配置文件，仅供测试开关使用。
	TLSTestMode bool `yaml:"-"`
}

// applyTestMode 根据环境变量 MESH_TEST_TLS 决定是否启用测试模式（自签证书）。
// 生产环境不设此变量，行为不受影响。
func (c *Config) applyTestMode() {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("MESH_TEST_TLS")))
	switch v {
	case "1", "true", "on", "self":
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
		Mode:       ModeFull,
		TLSMode:    TLSAutocert,
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
	cfg.normalizeMode()
	cfg.normalizeTLSMode()
	return cfg, nil
}

// normalizeMode 把 Mode 归一化为合法值。空或非法值回退 full 并打印告警,
// 避免配置笔误让进程进入未定义状态。warning 风格仿 server.loadCfg。
func (c *Config) normalizeMode() {
	switch strings.ToLower(strings.TrimSpace(c.Mode)) {
	case "", ModeFull:
		c.Mode = ModeFull
	case ModeRelay:
		c.Mode = ModeRelay
	default:
		fmt.Fprintf(os.Stderr, "warning: unknown mode %q, falling back to %q\n", c.Mode, ModeFull)
		c.Mode = ModeFull
	}
}

// normalizeTLSMode 把 TLSMode 归一化。空或非法值回退 autocert 并打印告警。
// TLSTestMode(env)优先级更高,此处只处理 yaml 的 tls_mode。
func (c *Config) normalizeTLSMode() {
	switch strings.ToLower(strings.TrimSpace(c.TLSMode)) {
	case "", TLSAutocert:
		c.TLSMode = TLSAutocert
	case TLSNone:
		c.TLSMode = TLSNone
	default:
		fmt.Fprintf(os.Stderr, "warning: unknown tls_mode %q, falling back to %q\n", c.TLSMode, TLSAutocert)
		c.TLSMode = TLSAutocert
	}
}
