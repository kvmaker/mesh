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
