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

func TestLoadTLSTestModeFromEnv(t *testing.T) {
	cases := []struct {
		name string
		env  string
		want bool
	}{
		{"off", "off", true},
		{"empty", "", false},
		{"true", "true", true},
		{"self", "self", true},
		{"no_invalid", "no", false},
		{"uppercase_off", "OFF", true},
		{"trim_spaces", " off ", true},
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
