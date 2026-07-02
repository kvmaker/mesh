package tests

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/maxyu/mesh/internal/api"
	"github.com/maxyu/mesh/internal/config"
	"github.com/maxyu/mesh/internal/db"
	"github.com/maxyu/mesh/internal/device"
	"github.com/maxyu/mesh/internal/token"
	"github.com/maxyu/mesh/internal/wg"
)

func TestFullRegistrationFlow(t *testing.T) {
	// 初始化内存数据库
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()
	db.Migrate(database)

	// 设置 token
	tok, err := token.Generate()
	if err != nil {
		t.Fatalf("generate token: %v", err)
	}
	if err := token.Save(database, tok); err != nil {
		t.Fatalf("save token: %v", err)
	}

	// 配置
	cfg := config.Default()
	cfg.Endpoint = "test.example.com:51820"

	// 创建 API 服务器（使用测试接口名，跳过真实 WireGuard）
	srv := api.New(database, cfg, "wg0-test")

	// 模拟客户端注册
	_, pubKey, err := wg.GenerateKeyPair()
	if err != nil {
		t.Fatalf("generate key pair: %v", err)
	}
	reqBody, _ := json.Marshal(map[string]string{
		"token":      tok,
		"public_key": pubKey,
		"hostname":   "test-device",
	})

	req := httptest.NewRequest("POST", "/api/devices/register", bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("register failed: %d %s", w.Code, w.Body.String())
	}

	var resp struct {
		AssignedIP   string `json:"assigned_ip"`
		DeviceSecret string `json:"device_secret"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("parse register response: %v", err)
	}

	if resp.AssignedIP != "10.100.0.2" {
		t.Fatalf("expected 10.100.0.2, got %s", resp.AssignedIP)
	}
	if resp.DeviceSecret == "" {
		t.Fatal("device_secret should not be empty")
	}

	// 验证心跳
	hbReq := httptest.NewRequest("POST", "/api/devices/heartbeat", nil)
	hbReq.Header.Set("Authorization", "Bearer "+resp.DeviceSecret)
	hbW := httptest.NewRecorder()
	srv.Handler().ServeHTTP(hbW, hbReq)

	if hbW.Code != http.StatusOK {
		t.Fatalf("heartbeat failed: %d %s", hbW.Code, hbW.Body.String())
	}

	// 验证设备列表
	devices, err := device.List(database)
	if err != nil {
		t.Fatalf("list devices: %v", err)
	}
	if len(devices) != 1 {
		t.Fatalf("expected 1 device, got %d", len(devices))
	}
	if !devices[0].Online {
		t.Fatal("device should be online after heartbeat")
	}
}

func TestMultipleDevicesGetDifferentIPs(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()
	db.Migrate(database)

	tok, err := token.Generate()
	if err != nil {
		t.Fatalf("generate token: %v", err)
	}
	if err := token.Save(database, tok); err != nil {
		t.Fatalf("save token: %v", err)
	}

	cfg := config.Default()
	cfg.Endpoint = "test.example.com:51820"
	srv := api.New(database, cfg, "wg0-test")

	// 注册 3 台设备
	expectedIPs := []string{"10.100.0.2", "10.100.0.3", "10.100.0.4"}
	for i, expected := range expectedIPs {
		_, pubKey, err := wg.GenerateKeyPair()
		if err != nil {
			t.Fatalf("generate key pair for device %d: %v", i, err)
		}
		reqBody, _ := json.Marshal(map[string]string{
			"token":      tok,
			"public_key": pubKey,
			"hostname":   fmt.Sprintf("device-%d", i),
		})

		req := httptest.NewRequest("POST", "/api/devices/register", bytes.NewReader(reqBody))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		srv.Handler().ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("device %d register failed: %d %s", i, w.Code, w.Body.String())
		}

		var resp struct {
			AssignedIP string `json:"assigned_ip"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("parse response for device %d: %v", i, err)
		}

		if resp.AssignedIP != expected {
			t.Fatalf("device %d: expected %s, got %s", i, expected, resp.AssignedIP)
		}
	}
}
