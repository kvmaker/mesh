package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefault(t *testing.T) {
	cfg := Default()
	if cfg.TunMTU != 1300 {
		t.Fatalf("expected MTU 1300, got %d", cfg.TunMTU)
	}
}

func TestLoad(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "cfg.yaml")
	os.WriteFile(p, []byte("domain: test.com\ntun_mtu: 1400\n"), 0644)
	cfg, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Domain != "test.com" || cfg.TunMTU != 1400 {
		t.Fatalf("unexpected: %+v", cfg)
	}
}

// TestLoadFileNotExist 覆盖 Load 的 read 错误分支：文件不存在返回错误。
func TestLoadFileNotExist(t *testing.T) {
	_, err := Load(filepath.Join(t.TempDir(), "nonexistent.yaml"))
	if err == nil {
		t.Fatal("expected error loading nonexistent config")
	}
}

// TestLoadInvalidYAML 覆盖 Load 的 parse 错误分支：非法 YAML 返回错误。
func TestLoadInvalidYAML(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "bad.yaml")
	// 用一个类型不匹配的值触发 yaml.Unmarshal 失败（tun_mtu 期望 int，给字符串）。
	os.WriteFile(p, []byte("tun_mtu: [not, an, int]\n"), 0644)
	_, err := Load(p)
	if err == nil {
		t.Fatal("expected error parsing invalid yaml")
	}
}

// TestLoadKeepsDefaults 验证 Load 未指定的字段回退到 Default 值。
func TestLoadKeepsDefaults(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "partial.yaml")
	os.WriteFile(p, []byte("domain: partial.com\n"), 0644)
	cfg, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Domain != "partial.com" {
		t.Fatalf("expected domain partial.com, got %s", cfg.Domain)
	}
	// 未指定的字段应保留 Default 值。
	if cfg.TunMTU != 1300 {
		t.Fatalf("expected default MTU 1300, got %d", cfg.TunMTU)
	}
	if cfg.Network != "10.100.0.0/24" {
		t.Fatalf("expected default network, got %s", cfg.Network)
	}
}

func TestLoadTLSTestModeFromEnv(t *testing.T) {
	cases := []struct {
		name string
		env  string
		want bool
	}{
		{"off", "off", false},
		{"empty", "", false},
		{"true", "true", true},
		{"self", "self", true},
		{"no_invalid", "no", false},
		{"uppercase_off", "OFF", false},
		{"trim_spaces", " off ", false},
		{"on", "on", true},
		{"1", "1", true},
		{"garbage", "garbage", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("MESH_TEST_TLS", tc.env)
			cfg := Default()
			if cfg.TLSTestMode != tc.want {
				t.Fatalf("MESH_TEST_TLS=%q: expected TLSTestMode=%v, got %v", tc.env, tc.want, cfg.TLSTestMode)
			}
		})
	}
}

func TestModeDefault(t *testing.T) {
	cfg := Default()
	if cfg.Mode != ModeFull {
		t.Fatalf("expected default Mode=%q, got %q", ModeFull, cfg.Mode)
	}
}

func TestModeLoadValues(t *testing.T) {
	cases := []struct {
		yaml string
		want string
	}{
		{"mode: full\n", ModeFull},
		{"mode: relay\n", ModeRelay},
		{"mode: RELAY\n", ModeRelay},   // 大小写不敏感
		{"mode:  relay \n", ModeRelay}, // 容忍空白
		{"domain: x.com\n", ModeFull},  // 未指定 → 默认 full
		{"mode: foo\n", ModeFull},      // 非法 → 回退 full
		{"mode: \n", ModeFull},         // 空值 → full
	}
	for _, tc := range cases {
		t.Run(tc.yaml, func(t *testing.T) {
			p := filepath.Join(t.TempDir(), "cfg.yaml")
			os.WriteFile(p, []byte(tc.yaml), 0644)
			cfg, err := Load(p)
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if cfg.Mode != tc.want {
				t.Fatalf("yaml=%q: expected Mode=%q, got %q", tc.yaml, tc.want, cfg.Mode)
			}
		})
	}
}
