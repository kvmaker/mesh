package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefault(t *testing.T) {
	cfg := Default()
	if cfg.ListenPort != 51820 {
		t.Fatalf("expected port 51820, got %d", cfg.ListenPort)
	}
	if cfg.APIPort != 8080 {
		t.Fatalf("expected api port 8080, got %d", cfg.APIPort)
	}
	if cfg.Network != "10.100.0.0/24" {
		t.Fatalf("expected 10.100.0.0/24, got %s", cfg.Network)
	}
}

func TestLoadFromFile(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "meshd.yaml")
	content := []byte(`endpoint: "example.com:51820"
listen_port: 51821
api_port: 9090
network: "10.200.0.0/24"
data_dir: "/var/mesh"
`)
	os.WriteFile(cfgPath, content, 0644)

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if cfg.Endpoint != "example.com:51820" {
		t.Fatalf("expected example.com:51820, got %s", cfg.Endpoint)
	}
	if cfg.ListenPort != 51821 {
		t.Fatalf("expected 51821, got %d", cfg.ListenPort)
	}
	if cfg.APIPort != 9090 {
		t.Fatalf("expected 9090, got %d", cfg.APIPort)
	}
	if cfg.Network != "10.200.0.0/24" {
		t.Fatalf("expected 10.200.0.0/24, got %s", cfg.Network)
	}
	if cfg.DataDir != "/var/mesh" {
		t.Fatalf("expected /var/mesh, got %s", cfg.DataDir)
	}
}

func TestLoadMissing(t *testing.T) {
	_, err := Load("/nonexistent/meshd.yaml")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}
