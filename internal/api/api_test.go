package api

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/maxyu/mesh/internal/config"
	"github.com/maxyu/mesh/internal/db"
	"github.com/maxyu/mesh/internal/token"
)

func setupTest(t *testing.T) (*Server, *sql.DB) {
	t.Helper()
	d, _ := db.Open(filepath.Join(t.TempDir(), "test.db"))
	db.Migrate(d) //nolint:errcheck
	t.Cleanup(func() { d.Close() })
	tok, _ := token.Generate()
	token.Save(d, tok) //nolint:errcheck
	cfg := config.Default()
	srv := New(d, cfg, nil)
	return srv, d
}

func TestRegisterSuccess(t *testing.T) {
	srv, d := setupTest(t)
	tok, _ := token.Load(d)

	body, _ := json.Marshal(RegisterRequest{Token: tok, Hostname: "test-device"})
	req := httptest.NewRequest("POST", "/api/devices/register", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp RegisterResponse
	json.Unmarshal(w.Body.Bytes(), &resp) //nolint:errcheck
	if resp.AssignedIP != "10.100.0.2" {
		t.Fatalf("expected 10.100.0.2, got %s", resp.AssignedIP)
	}
	if resp.DeviceSecret == "" {
		t.Fatal("expected non-empty secret")
	}
}

func TestRegisterInvalidToken(t *testing.T) {
	srv, _ := setupTest(t)
	body, _ := json.Marshal(RegisterRequest{Token: "wrong", Hostname: "test"})
	req := httptest.NewRequest("POST", "/api/devices/register", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != 401 {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestCoverPage(t *testing.T) {
	srv, _ := setupTest(t)
	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "text/html" {
		t.Fatalf("expected text/html, got %s", ct)
	}
}

func TestTunnelNilGuard(t *testing.T) {
	srv, _ := setupTest(t)
	req := httptest.NewRequest("GET", "/tunnel", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 when tunnel is nil, got %d", w.Code)
	}
}

func TestRegisterRateLimit(t *testing.T) {
	srv, d := setupTest(t)
	tok, _ := token.Load(d)

	for i := 0; i < 5; i++ {
		body, _ := json.Marshal(RegisterRequest{Token: tok, Hostname: "dev"})
		req := httptest.NewRequest("POST", "/api/devices/register", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.RemoteAddr = "1.2.3.4:5678"
		w := httptest.NewRecorder()
		srv.Handler().ServeHTTP(w, req)
	}

	// 6th request from same IP should be rate-limited
	body, _ := json.Marshal(RegisterRequest{Token: tok, Hostname: "dev"})
	req := httptest.NewRequest("POST", "/api/devices/register", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "1.2.3.4:5678"
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d", w.Code)
	}
}
