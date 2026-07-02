package api

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/maxyu/mesh/internal/config"
	"github.com/maxyu/mesh/internal/db"
	"github.com/maxyu/mesh/internal/token"
)

func setupTestServer(t *testing.T) (*Server, *sql.DB) {
	t.Helper()
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	db.Migrate(database)
	t.Cleanup(func() { database.Close() })

	tok, _ := token.Generate()
	token.Save(database, tok)

	cfg := config.Default()
	cfg.Endpoint = "test.example.com:51820"

	srv := New(database, cfg, "wg0-test")
	return srv, database
}

func TestRegisterSuccess(t *testing.T) {
	srv, database := setupTestServer(t)
	tok, _ := token.Load(database)

	body := RegisterRequest{
		Token:     tok,
		PublicKey: "dGVzdHB1YmtleTEyMzQ1Njc4OTAxMjM0NTY=", // valid base64
		Hostname:  "test-macbook",
	}
	jsonBody, _ := json.Marshal(body)

	req := httptest.NewRequest("POST", "/api/devices/register", bytes.NewReader(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	srv.handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp RegisterResponse
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.AssignedIP != "10.100.0.2" {
		t.Fatalf("expected 10.100.0.2, got %s", resp.AssignedIP)
	}
	if resp.DeviceSecret == "" {
		t.Fatal("expected non-empty device secret")
	}
}

func TestRegisterInvalidToken(t *testing.T) {
	srv, _ := setupTestServer(t)

	body := RegisterRequest{
		Token:     "invalid-token",
		PublicKey: "dGVzdHB1YmtleTEyMzQ1Njc4OTAxMjM0NTY=",
		Hostname:  "test",
	}
	jsonBody, _ := json.Marshal(body)

	req := httptest.NewRequest("POST", "/api/devices/register", bytes.NewReader(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	srv.handler().ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestRegisterDuplicatePublicKey(t *testing.T) {
	srv, database := setupTestServer(t)
	tok, _ := token.Load(database)

	body := RegisterRequest{
		Token:     tok,
		PublicKey: "dGVzdHB1YmtleTEyMzQ1Njc4OTAxMjM0NTY=",
		Hostname:  "test",
	}
	jsonBody, _ := json.Marshal(body)

	// 第一次注册
	req := httptest.NewRequest("POST", "/api/devices/register", bytes.NewReader(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.handler().ServeHTTP(w, req)

	// 第二次注册（幂等，返回已有信息）
	req = httptest.NewRequest("POST", "/api/devices/register", bytes.NewReader(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	srv.handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for idempotent register, got %d", w.Code)
	}
}

func TestHeartbeat(t *testing.T) {
	srv, database := setupTestServer(t)
	tok, _ := token.Load(database)

	// 先注册
	body := RegisterRequest{Token: tok, PublicKey: "dGVzdHB1YmtleTEyMzQ1Njc4OTAxMjM0NTY=", Hostname: "test"}
	jsonBody, _ := json.Marshal(body)
	req := httptest.NewRequest("POST", "/api/devices/register", bytes.NewReader(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.handler().ServeHTTP(w, req)

	var regResp RegisterResponse
	json.Unmarshal(w.Body.Bytes(), &regResp)

	// 发送心跳
	req = httptest.NewRequest("POST", "/api/devices/heartbeat", nil)
	req.Header.Set("Authorization", "Bearer "+regResp.DeviceSecret)
	w = httptest.NewRecorder()
	srv.handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}
