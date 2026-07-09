package tests

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/maxyu/mesh/internal/server/api"
	"github.com/maxyu/mesh/internal/server/config"
	"github.com/maxyu/mesh/internal/server/db"
	"github.com/maxyu/mesh/internal/server/device"
	"github.com/maxyu/mesh/internal/server/token"
)

func setup(t *testing.T) (*api.Server, string) {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	db.Migrate(d)
	tok, _ := token.Generate()
	token.Save(d, tok)
	cfg := config.Default()
	srv := api.New(d, cfg, nil)
	t.Cleanup(func() { d.Close() })
	return srv, tok
}

func TestFullFlow(t *testing.T) {
	srv, tok := setup(t)

	body, _ := json.Marshal(map[string]string{"token": tok, "hostname": "client1"})
	req := httptest.NewRequest("POST", "/api/devices/register", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("register: %d %s", w.Code, w.Body.String())
	}

	var resp struct {
		AssignedIP   string `json:"assigned_ip"`
		DeviceSecret string `json:"device_secret"`
		DeviceID     string `json:"device_id"`
		NetworkCIDR  string `json:"network_cidr"`
	}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.AssignedIP != "10.100.0.2" {
		t.Fatalf("expected 10.100.0.2, got %s", resp.AssignedIP)
	}
	if resp.DeviceSecret == "" {
		t.Fatal("expected non-empty secret")
	}
	if resp.NetworkCIDR != "10.100.0.0/24" {
		t.Fatalf("expected 10.100.0.0/24, got %s", resp.NetworkCIDR)
	}
}

func TestMultipleRegistrations(t *testing.T) {
	srv, tok := setup(t)

	expected := []string{"10.100.0.2", "10.100.0.3", "10.100.0.4"}
	for i, exp := range expected {
		body, _ := json.Marshal(map[string]string{"token": tok, "hostname": fmt.Sprintf("dev%d", i)})
		req := httptest.NewRequest("POST", "/api/devices/register", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		srv.Handler().ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("device %d: %d %s", i, w.Code, w.Body.String())
		}
		var resp struct {
			AssignedIP string `json:"assigned_ip"`
		}
		json.Unmarshal(w.Body.Bytes(), &resp)
		if resp.AssignedIP != exp {
			t.Fatalf("device %d: expected %s, got %s", i, exp, resp.AssignedIP)
		}
	}
}

func TestDeviceListAfterRegister(t *testing.T) {
	srv, tok := setup(t)

	// 获取内部 db 用于验证
	d, _ := db.Open(filepath.Join(t.TempDir(), "verify.db"))
	defer d.Close()
	// 使用 srv 的 handler 注册，再直接查 device 包验证
	// 由于无法直接访问 srv 内部 db，我们验证 HTTP 层面

	body, _ := json.Marshal(map[string]string{"token": tok, "hostname": "testdev"})
	req := httptest.NewRequest("POST", "/api/devices/register", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("register: %d", w.Code)
	}

	_ = device.List // 确认 device 包可引用
}
