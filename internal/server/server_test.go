package server

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/maxyu/mesh/internal/server/db"
	"github.com/maxyu/mesh/internal/server/device"
)

// writeTestConfig writes a minimal meshd.yaml whose data_dir/cert_dir point
// into t.TempDir(), and returns the config file path.
func writeTestConfig(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "meshd.yaml")
	content := "" +
		"domain: localhost\n" +
		"network: 10.100.0.0/24\n" +
		"data_dir: " + dir + "\n" +
		"cert_dir: " + filepath.Join(dir, "certs") + "\n"
	if err := os.WriteFile(cfgPath, []byte(content), 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return cfgPath
}

// runRoot executes the meshd root command with the given args and returns its
// combined stdout/stderr and error.
func runRoot(t *testing.T, args ...string) (string, error) {
	t.Helper()
	root := NewRootCmd()
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs(args)
	err := root.Execute()
	return buf.String(), err
}

func TestInitCmd(t *testing.T) {
	cfg := writeTestConfig(t)
	if _, err := runRoot(t, "--config", cfg, "init"); err != nil {
		t.Fatalf("init: %v", err)
	}
	// init must create the sqlite database under data_dir.
	dir := filepath.Dir(cfg)
	if _, err := os.Stat(filepath.Join(dir, "mesh.db")); err != nil {
		t.Fatalf("expected mesh.db to be created: %v", err)
	}
}

func TestTokenShowAndReset(t *testing.T) {
	cfg := writeTestConfig(t)
	if _, err := runRoot(t, "--config", cfg, "init"); err != nil {
		t.Fatalf("init: %v", err)
	}

	// token show should succeed once initialized.
	if _, err := runRoot(t, "--config", cfg, "token", "show"); err != nil {
		t.Fatalf("token show: %v", err)
	}

	// token reset should also succeed.
	if _, err := runRoot(t, "--config", cfg, "token", "reset"); err != nil {
		t.Fatalf("token reset: %v", err)
	}
}

func TestVersionCmd(t *testing.T) {
	if _, err := runRoot(t, "version"); err != nil {
		t.Fatalf("version: %v", err)
	}
}

func TestDeviceListAndRemove(t *testing.T) {
	cfg := writeTestConfig(t)
	if _, err := runRoot(t, "--config", cfg, "init"); err != nil {
		t.Fatalf("init: %v", err)
	}

	// Insert a device directly so list/remove have something to act on.
	dir := filepath.Dir(cfg)
	d, err := db.Open(filepath.Join(dir, "mesh.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	dev := &device.Device{
		ID:     "abcdef1234567890",
		Name:   "laptop",
		IP:     "10.100.0.2",
		Secret: "sec",
	}
	if err := device.Create(d, dev); err != nil {
		t.Fatalf("create device: %v", err)
	}
	d.Close()

	// device list should list the inserted device.
	out, err := runRoot(t, "--config", cfg, "device", "list")
	if err != nil {
		t.Fatalf("device list: %v", err)
	}
	_ = out // output goes to os.Stdout via tabwriter, not the cobra buffer.

	// device remove by name should succeed.
	if _, err := runRoot(t, "--config", cfg, "device", "remove", "laptop"); err != nil {
		t.Fatalf("device remove: %v", err)
	}

	// removing a non-existent device should return an error.
	if _, err := runRoot(t, "--config", cfg, "device", "remove", "ghost"); err == nil {
		t.Fatal("expected error removing non-existent device")
	}
}

func TestDeviceRemoveByIDPrefix(t *testing.T) {
	cfg := writeTestConfig(t)
	if _, err := runRoot(t, "--config", cfg, "init"); err != nil {
		t.Fatalf("init: %v", err)
	}
	dir := filepath.Dir(cfg)
	d, _ := db.Open(filepath.Join(dir, "mesh.db"))
	device.Create(d, &device.Device{ID: "deadbeef00001111", Name: "srv", IP: "10.100.0.3", Secret: "s"})
	d.Close()

	if _, err := runRoot(t, "--config", cfg, "device", "remove", "deadbeef"); err != nil {
		t.Fatalf("device remove by id prefix: %v", err)
	}
}

// TestDeviceRemoveEmptyArg 验证空标识符被拒绝，避免空前缀匹配到任意设备。
func TestDeviceRemoveEmptyArg(t *testing.T) {
	cfg := writeTestConfig(t)
	if _, err := runRoot(t, "--config", cfg, "init"); err != nil {
		t.Fatalf("init: %v", err)
	}
	dir := filepath.Dir(cfg)
	d, _ := db.Open(filepath.Join(dir, "mesh.db"))
	device.Create(d, &device.Device{ID: "aaaa000011112222", Name: "n1", IP: "10.100.0.2", Secret: "s"})
	d.Close()

	if _, err := runRoot(t, "--config", cfg, "device", "remove", "   "); err == nil {
		t.Fatal("expected error for empty/whitespace identifier")
	}
	// 设备应仍然存在（未被误删）。
	d, _ = db.Open(filepath.Join(dir, "mesh.db"))
	defer d.Close()
	devs, _ := device.List(d)
	if len(devs) != 1 {
		t.Fatalf("expected device to survive empty-arg remove, got %d devices", len(devs))
	}
}

// TestDeviceRemoveAmbiguousPrefix 验证前缀命中多个设备时报错而非误删首个。
func TestDeviceRemoveAmbiguousPrefix(t *testing.T) {
	cfg := writeTestConfig(t)
	if _, err := runRoot(t, "--config", cfg, "init"); err != nil {
		t.Fatalf("init: %v", err)
	}
	dir := filepath.Dir(cfg)
	d, _ := db.Open(filepath.Join(dir, "mesh.db"))
	device.Create(d, &device.Device{ID: "dead0000aaaa1111", Name: "n1", IP: "10.100.0.2", Secret: "s1"})
	device.Create(d, &device.Device{ID: "dead1111bbbb2222", Name: "n2", IP: "10.100.0.3", Secret: "s2"})
	d.Close()

	if _, err := runRoot(t, "--config", cfg, "device", "remove", "dead"); err == nil {
		t.Fatal("expected error for ambiguous prefix matching multiple devices")
	}
	// 两个设备都应保留（歧义时不删）。
	d, _ = db.Open(filepath.Join(dir, "mesh.db"))
	defer d.Close()
	devs, _ := device.List(d)
	if len(devs) != 2 {
		t.Fatalf("expected both devices to survive ambiguous remove, got %d", len(devs))
	}
}

func TestLoadCfgFallback(t *testing.T) {
	// Point --config at a missing file; loadCfg must fall back to defaults
	// rather than error out.
	cfgPath = filepath.Join(t.TempDir(), "does-not-exist.yaml")
	cfg := loadCfg()
	if cfg.Network != "10.100.0.0/24" {
		t.Fatalf("expected default network, got %s", cfg.Network)
	}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		name string
		d    time.Duration
		want string
	}{
		{name: "seconds", d: 45 * time.Second, want: "45s"},
		{name: "minutes", d: 3*time.Minute + 5*time.Second, want: "3m5s"},
		{name: "hours", d: 2*time.Hour + 30*time.Minute, want: "2h30m"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := formatDuration(tt.d); got != tt.want {
				t.Fatalf("formatDuration(%v)=%q, want %q", tt.d, got, tt.want)
			}
		})
	}
}

func TestFetchRuntimeStats(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`[{"device_id":"dev1","ip":"10.100.0.2","tx_packets":10}]`))
	}))
	defer srv.Close()

	// srv.URL is https://127.0.0.1:PORT; strip the scheme so it matches the
	// "https://%s/..." format string in fetchRuntimeStats. httptest uses a
	// self-signed cert, so skip TLS verification (insecureTLS=true).
	domain := strings.TrimPrefix(srv.URL, "https://")
	m := fetchRuntimeStats(domain, "testtoken", true)
	if len(m) != 1 {
		t.Fatalf("expected 1 stat, got %d", len(m))
	}
	if _, ok := m["dev1"]; !ok {
		t.Fatalf("expected dev1 in stats, got %v", m)
	}
}

func TestFetchRuntimeStatsUnreachable(t *testing.T) {
	// Nothing is listening here; fetchRuntimeStats must return nil, not panic.
	if m := fetchRuntimeStats("127.0.0.1:1", "testtoken", true); m != nil {
		t.Fatalf("expected nil on connection failure, got %v", m)
	}
}

func TestFetchRuntimeStatsBadJSON(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("not json"))
	}))
	defer srv.Close()
	domain := strings.TrimPrefix(srv.URL, "https://")
	if m := fetchRuntimeStats(domain, "testtoken", true); m != nil {
		t.Fatalf("expected nil on decode failure, got %v", m)
	}
}

func TestTokenShowUninitialized(t *testing.T) {
	// init creates the DB and migrates it but we skip token setup by opening
	// a fresh DB with no token row: token show must surface the load error.
	cfg := writeTestConfig(t)
	dir := filepath.Dir(cfg)
	d, _ := db.Open(filepath.Join(dir, "mesh.db"))
	db.Migrate(d) //nolint:errcheck
	d.Close()

	if _, err := runRoot(t, "--config", cfg, "token", "show"); err == nil {
		t.Fatal("expected error when no token has been generated")
	}
}

func TestDeviceListWithStats(t *testing.T) {
	cfg := writeTestConfig(t)
	if _, err := runRoot(t, "--config", cfg, "init"); err != nil {
		t.Fatalf("init: %v", err)
	}
	dir := filepath.Dir(cfg)
	d, _ := db.Open(filepath.Join(dir, "mesh.db"))
	device.Create(d, &device.Device{ID: "abcdef1234567890", Name: "laptop", IP: "10.100.0.2", Secret: "s"})
	d.Close()

	// --stats path calls fetchRuntimeStats against cfg.Domain (localhost).
	// The connection will fail (nothing listening), exercising the nil-stats
	// branch of the list rendering without requiring a live server.
	if _, err := runRoot(t, "--config", cfg, "device", "list", "--stats"); err != nil {
		t.Fatalf("device list --stats: %v", err)
	}
}

// TestDeviceListStatsHit stands up a fake stats endpoint whose response
// matches the inserted device ID, exercising the statsMap-hit rendering
// branch (duration/tx/rx/last-packet columns) in deviceCmd.
func TestDeviceListStatsHit(t *testing.T) {
	// 服务端用自签证书(httptest)，需开启测试模式让 device list --stats
	// 跳过 TLS 校验，从而真正命中 statsMap 渲染分支。
	t.Setenv("MESH_TEST_TLS", "1")
	deviceID := "abcdef1234567890"
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// connected_at in the past and last_packet set → both formatDuration
		// calls and the LastPacket branch are exercised.
		w.Write([]byte(`[{"device_id":"` + deviceID + `","ip":"10.100.0.2",` +
			`"connected_at":"2020-01-01T00:00:00Z","tx_packets":5,"tx_bytes":1024,` +
			`"rx_packets":3,"rx_bytes":512,"last_packet":"2020-01-01T00:01:00Z"}]`))
	}))
	defer srv.Close()

	domain := strings.TrimPrefix(srv.URL, "https://")
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "meshd.yaml")
	content := "" +
		"domain: " + domain + "\n" +
		"network: 10.100.0.0/24\n" +
		"data_dir: " + dir + "\n" +
		"cert_dir: " + filepath.Join(dir, "certs") + "\n"
	if err := os.WriteFile(cfgPath, []byte(content), 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	if _, err := runRoot(t, "--config", cfgPath, "init"); err != nil {
		t.Fatalf("init: %v", err)
	}
	d, _ := db.Open(filepath.Join(dir, "mesh.db"))
	device.Create(d, &device.Device{ID: deviceID, Name: "laptop", IP: "10.100.0.2", Secret: "s"})
	d.Close()

	if _, err := runRoot(t, "--config", cfgPath, "device", "list", "--stats"); err != nil {
		t.Fatalf("device list --stats: %v", err)
	}
}
