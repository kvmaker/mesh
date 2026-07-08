package client

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// registerHandler 返回一个固定的注册成功响应。
func registerHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprint(w, `{"assigned_ip":"10.100.0.5","device_secret":"s","device_id":"d1","network_cidr":"10.100.0.0/24"}`)
}

// TestJoinInsecureTLS 覆盖 Join 的 insecure 分支：
//   - insecure=true 能连上自签 TLS server（间接证明设置了 InsecureSkipVerify），
//     并把 InsecureTLS=true 持久化到 config。
//   - insecure=false 面对自签证书应当连接失败且不保存 config（证明未跳过校验，
//     生产路径安全）。
//
// 用 t.Setenv("HOME", ...) 隔离 ConfigDir，避免污染本机 ~/.mesh/config.json。
func TestJoinInsecureTLS(t *testing.T) {
	// httptest.NewTLSServer 使用自签证书，默认客户端校验会失败。
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/devices/register", registerHandler)
	srv := httptest.NewTLSServer(mux)
	defer srv.Close()

	domain := strings.TrimPrefix(srv.URL, "https://")

	t.Run("insecure_true_connects_and_persists", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		if err := Join(domain, "tok", true); err != nil {
			t.Fatalf("Join insecure=true failed: %v", err)
		}
		cfg, err := LoadClientConfig()
		if err != nil {
			t.Fatalf("LoadClientConfig: %v", err)
		}
		if !cfg.InsecureTLS {
			t.Fatal("expected InsecureTLS=true persisted in config")
		}
	})

	t.Run("insecure_false_rejects_self_signed", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		// 自签证书 + insecure=false：连接应因证书校验失败而失败，且不保存 config。
		if err := Join(domain, "tok", false); err == nil {
			t.Fatal("expected Join to fail against self-signed server when insecure=false")
		}
		if _, err := LoadClientConfig(); err == nil {
			t.Fatal("expected no config saved when Join fails on cert verification")
		}
	})
}
