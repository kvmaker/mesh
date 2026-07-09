package api

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/maxyu/mesh/internal/server/config"
	"github.com/maxyu/mesh/internal/server/db"
	"github.com/maxyu/mesh/internal/server/device"
	"github.com/maxyu/mesh/internal/server/token"
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

// Create inserts a device at 10.100.0.<host> for list/exhaustion tests.
func Create(t *testing.T, d *sql.DB, host int) {
	t.Helper()
	dev := &device.Device{
		ID:     fmt.Sprintf("id%d", host),
		Name:   fmt.Sprintf("dev%d", host),
		IP:     fmt.Sprintf("10.100.0.%d", host),
		Secret: fmt.Sprintf("sec%d", host),
	}
	if err := device.Create(d, dev); err != nil {
		t.Fatalf("device.Create: %v", err)
	}
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

func TestRegisterInvalidBody(t *testing.T) {
	srv, _ := setupTest(t)
	req := httptest.NewRequest("POST", "/api/devices/register", bytes.NewReader([]byte("not json")))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestRegisterNoAvailableIPs(t *testing.T) {
	srv, d := setupTest(t)
	// Shrink the pool to a single usable host so the second registration
	// exhausts it and hits the 503 branch.
	srv.cfg.Network = "10.100.0.0/30" // usable hosts: .2 only within 2..254 loop
	tok, _ := token.Load(d)

	// First registration should succeed and take .2.
	body, _ := json.Marshal(RegisterRequest{Token: tok, Hostname: "dev1"})
	req := httptest.NewRequest("POST", "/api/devices/register", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("first register expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Manually fill .3 through .254 so Allocate finds nothing free.
	for i := 3; i <= 254; i++ {
		Create(t, d, i)
	}

	body, _ = json.Marshal(RegisterRequest{Token: tok, Hostname: "dev2"})
	req = httptest.NewRequest("POST", "/api/devices/register", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 when pool exhausted, got %d", w.Code)
	}
}

func TestHandleDeviceList(t *testing.T) {
	srv, d := setupTest(t)
	Create(t, d, 2)
	Create(t, d, 3)

	tok, _ := token.Load(d)
	req := httptest.NewRequest("GET", "/api/devices", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("expected application/json, got %s", ct)
	}
	var list []deviceInfo
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 devices, got %d", len(list))
	}
}

// TestHandleDeviceListDeviceSecretAuth 验证持设备密钥（而非 admin token）
// 的调用方也能访问受保护读接口——这是 client `mesh peers` 的鉴权路径。
func TestHandleDeviceListDeviceSecretAuth(t *testing.T) {
	srv, d := setupTest(t)
	Create(t, d, 2)

	req := httptest.NewRequest("GET", "/api/devices", nil)
	req.Header.Set("Authorization", "Bearer sec2") // Create 写入的设备密钥
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 with device secret, got %d", w.Code)
	}
}

// TestHandleDeviceListUnauthorized 验证缺失/无效凭证一律 401。
func TestHandleDeviceListUnauthorized(t *testing.T) {
	srv, d := setupTest(t)
	Create(t, d, 2)

	// 无 Authorization 头。
	req := httptest.NewRequest("GET", "/api/devices", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("missing credential: expected 401, got %d", w.Code)
	}

	// 无效凭证（既非 admin token 也非任何设备密钥）。
	req = httptest.NewRequest("GET", "/api/devices", nil)
	req.Header.Set("Authorization", "Bearer bogus")
	w = httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("invalid credential: expected 401, got %d", w.Code)
	}
}

func TestHandleDeviceStatsNilTunnel(t *testing.T) {
	srv, d := setupTest(t) // tunnel is nil
	tok, _ := token.Load(d)
	req := httptest.NewRequest("GET", "/api/stats/devices", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	// nil tunnel must yield an empty JSON array, not null.
	if got := w.Body.String(); got != "[]\n" {
		t.Fatalf("expected empty array, got %q", got)
	}
}

func TestSelfSignedTLSConfig(t *testing.T) {
	srv, _ := setupTest(t)
	tlsCfg, err := srv.selfSignedTLSConfig()
	if err != nil {
		t.Fatalf("selfSignedTLSConfig: %v", err)
	}
	if len(tlsCfg.Certificates) != 1 {
		t.Fatalf("expected 1 certificate, got %d", len(tlsCfg.Certificates))
	}
	if tlsCfg.MinVersion == 0 {
		t.Fatal("expected MinVersion to be set")
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
	resetLimiter() // 隔离全局 limiter，避免其它用例的历史计数污染本用例
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

// TestRegisterRateLimitIPv6 验证 IPv6 客户端地址被正确解析为独立 key：
// 用 net.SplitHostPort 后 [::1]:1111 与 [::2]:2222 是两个不同 IP，各自
// 独立计数；若仍用 strings.Split(":") 则会被错误归并到同一 key。
func TestRegisterRateLimitIPv6(t *testing.T) {
	srv, d := setupTest(t)
	resetLimiter()
	tok, _ := token.Load(d)

	send := func(remoteAddr string) int {
		body, _ := json.Marshal(RegisterRequest{Token: tok, Hostname: "dev"})
		req := httptest.NewRequest("POST", "/api/devices/register", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.RemoteAddr = remoteAddr
		w := httptest.NewRecorder()
		srv.Handler().ServeHTTP(w, req)
		return w.Code
	}

	// 用尽 [::1] 的 5 次配额。
	for i := 0; i < 5; i++ {
		send("[::1]:1111")
	}
	// [::1] 第 6 次应被限流。
	if code := send("[::1]:2222"); code != http.StatusTooManyRequests {
		t.Fatalf("[::1] 6th request: expected 429, got %d", code)
	}
	// 不同 IPv6 地址 [::2] 不受影响，应正常放行（非 429）。
	if code := send("[::2]:3333"); code == http.StatusTooManyRequests {
		t.Fatalf("[::2] should not be rate-limited (distinct IP), got 429")
	}
}

// TestRateLimiterSweepEvictsStaleIPs 验证 sweep 清理时间戳全部过期的 IP
// 条目，防止 requests map 随历史访问过的不同 IP 无界增长。
func TestRateLimiterSweepEvictsStaleIPs(t *testing.T) {
	resetLimiter()
	now := time.Now()

	limiter.mu.Lock()
	// stale：唯一时间戳早于一个窗口，应被清理。
	limiter.requests["1.1.1.1"] = []time.Time{now.Add(-2 * rateWindow)}
	// fresh：含一个窗口内的时间戳，应保留。
	limiter.requests["2.2.2.2"] = []time.Time{now.Add(-2 * rateWindow), now.Add(-time.Second)}
	// lastSweep 归零确保 sweep 不被节流跳过。
	limiter.lastSweep = time.Time{}
	limiter.sweepLocked(now)
	_, staleKept := limiter.requests["1.1.1.1"]
	_, freshKept := limiter.requests["2.2.2.2"]
	limiter.mu.Unlock()

	if staleKept {
		t.Fatal("stale IP with only expired timestamps should be evicted")
	}
	if !freshKept {
		t.Fatal("IP with a fresh timestamp must be retained")
	}
}

// TestRateLimiterSweepThrottled 验证 sweep 的节流：距上次清理不足一个
// 窗口时不执行全表扫描，stale 条目暂时保留（避免每请求 O(n) 开销）。
func TestRateLimiterSweepThrottled(t *testing.T) {
	resetLimiter()
	now := time.Now()

	limiter.mu.Lock()
	limiter.requests["1.1.1.1"] = []time.Time{now.Add(-2 * rateWindow)}
	// 假装刚刚清理过：节流应使本次 sweep 成为 no-op。
	limiter.lastSweep = now
	limiter.sweepLocked(now)
	_, kept := limiter.requests["1.1.1.1"]
	limiter.mu.Unlock()

	if !kept {
		t.Fatal("throttled sweep should not evict entries within the same window")
	}
}
