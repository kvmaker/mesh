package client

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestConfigDir(t *testing.T) {
	t.Setenv("HOME", "/tmp/fakehome")
	if got := ConfigDir(); got != "/tmp/fakehome/.mesh" {
		t.Fatalf("ConfigDir()=%q, want /tmp/fakehome/.mesh", got)
	}
}

func TestSaveAndLoadClientConfig(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cfg := &ClientConfig{
		ServerDomain: "example.com",
		DeviceSecret: "secret",
		DeviceIP:     "10.100.0.2",
		DeviceID:     "dev1",
		NetworkCIDR:  "10.100.0.0/24",
		InsecureTLS:  true,
	}
	if err := SaveClientConfig(cfg); err != nil {
		t.Fatalf("SaveClientConfig: %v", err)
	}
	got, err := LoadClientConfig()
	if err != nil {
		t.Fatalf("LoadClientConfig: %v", err)
	}
	if got.ServerDomain != "example.com" || got.DeviceIP != "10.100.0.2" || !got.InsecureTLS {
		t.Fatalf("round-trip mismatch: %+v", got)
	}
}

func TestLoadClientConfigMissing(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if _, err := LoadClientConfig(); err == nil {
		t.Fatal("expected error loading missing config")
	}
}

func TestSaveClientConfigMkdirError(t *testing.T) {
	home := t.TempDir()
	// Make HOME itself a regular file so MkdirAll(HOME/.mesh) fails.
	fakeHome := filepath.Join(home, "home-as-file")
	if err := os.WriteFile(fakeHome, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", fakeHome)
	if err := SaveClientConfig(&ClientConfig{ServerDomain: "x"}); err == nil {
		t.Fatal("expected MkdirAll error when HOME is a file")
	}
}

func TestLoadClientConfigInvalidJSON(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".mesh")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte("not json"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadClientConfig(); err == nil {
		t.Fatal("expected parse error for invalid JSON")
	}
}

func TestLeaveNotRegistered(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := Leave(); err == nil {
		t.Fatal("expected error when not registered")
	}
}

func TestLeaveRemovesConfig(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := SaveClientConfig(&ClientConfig{ServerDomain: "x"}); err != nil {
		t.Fatal(err)
	}
	if err := Leave(); err != nil {
		t.Fatalf("Leave: %v", err)
	}
	if _, err := os.Stat(ConfigDir()); !os.IsNotExist(err) {
		t.Fatalf("expected config dir removed, stat err=%v", err)
	}
}

func TestStatusNotRegistered(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := Status(); err == nil {
		t.Fatal("expected error when not registered")
	}
}

func TestStatusTunnelNotRunning(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := SaveClientConfig(&ClientConfig{ServerDomain: "x", DeviceIP: "10.100.0.2"}); err != nil {
		t.Fatal(err)
	}
	// No status.json → loadTunnelStats fails → "not running" path.
	if err := Status(); err != nil {
		t.Fatalf("Status: %v", err)
	}
}

func TestStatusStaleAndConnected(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := SaveClientConfig(&ClientConfig{ServerDomain: "x", DeviceIP: "10.100.0.2"}); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(home, ".mesh")

	writeStats := func(t *testing.T, s ClientStats) {
		t.Helper()
		data, _ := json.Marshal(s)
		if err := os.WriteFile(filepath.Join(dir, "status.json"), data, 0600); err != nil {
			t.Fatal(err)
		}
	}

	t.Run("stale", func(t *testing.T) {
		// Fresh PID but an old UpdatedAt → stale branch.
		writeStats(t, ClientStats{PID: os.Getpid(), UpdatedAt: time.Now().Add(-time.Hour)})
		if err := Status(); err != nil {
			t.Fatalf("Status stale: %v", err)
		}
	})

	t.Run("reconnecting", func(t *testing.T) {
		writeStats(t, ClientStats{PID: os.Getpid(), UpdatedAt: time.Now(), Connected: false})
		if err := Status(); err != nil {
			t.Fatalf("Status reconnecting: %v", err)
		}
	})

	t.Run("connected", func(t *testing.T) {
		writeStats(t, ClientStats{
			PID:         os.Getpid(),
			UpdatedAt:   time.Now(),
			Connected:   true,
			ConnectedAt: time.Now().Add(-90 * time.Second),
			TxPackets:   10,
			TxBytes:     2048,
			RxPackets:   8,
			RxBytes:     1024,
			LastRTT:     500, // < 1000 → μs branch
			LastActive:  time.Now().Add(-5 * time.Second),
		})
		if err := Status(); err != nil {
			t.Fatalf("Status connected: %v", err)
		}
	})

	t.Run("connected_ms_rtt", func(t *testing.T) {
		writeStats(t, ClientStats{
			PID:         os.Getpid(),
			UpdatedAt:   time.Now(),
			Connected:   true,
			ConnectedAt: time.Now().Add(-2 * time.Hour),
			LastRTT:     5000, // >= 1000 → ms branch
		})
		if err := Status(); err != nil {
			t.Fatalf("Status connected ms: %v", err)
		}
	})
}

func TestIsProcessAlive(t *testing.T) {
	if isProcessAlive(0) {
		t.Fatal("pid 0 should not be alive")
	}
	if isProcessAlive(-1) {
		t.Fatal("negative pid should not be alive")
	}
	if !isProcessAlive(os.Getpid()) {
		t.Fatal("current process should be alive")
	}
}

func TestLoadTunnelStatsMissing(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if _, err := loadTunnelStats(); err == nil {
		t.Fatal("expected error when status.json missing")
	}
}

func TestLoadTunnelStatsInvalid(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".mesh")
	os.MkdirAll(dir, 0700)
	os.WriteFile(filepath.Join(dir, "status.json"), []byte("bad"), 0600)
	if _, err := loadTunnelStats(); err == nil {
		t.Fatal("expected parse error for invalid status.json")
	}
}

func TestFormatDurationClient(t *testing.T) {
	tests := []struct {
		d    time.Duration
		want string
	}{
		{45 * time.Second, "45s"},
		{3*time.Minute + 5*time.Second, "3m5s"},
		{2*time.Hour + 30*time.Minute, "2h30m"},
	}
	for _, tt := range tests {
		if got := formatDuration(tt.d); got != tt.want {
			t.Fatalf("formatDuration(%v)=%q, want %q", tt.d, got, tt.want)
		}
	}
}

func TestPeersNotRegistered(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := Peers(); err == nil {
		t.Fatal("expected error when not registered")
	}
}

func TestPeers(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/devices", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`[{"name":"laptop","ip":"10.100.0.2","online":true},` +
			`{"name":"phone","ip":"10.100.0.3","online":false}]`))
	})
	srv := httptest.NewTLSServer(mux)
	defer srv.Close()
	domain := strings.TrimPrefix(srv.URL, "https://")

	t.Setenv("HOME", t.TempDir())
	// InsecureTLS=true so the self-signed test cert is accepted, and DeviceIP
	// matches one peer to exercise the "(me)" marker branch.
	if err := SaveClientConfig(&ClientConfig{
		ServerDomain: domain,
		DeviceIP:     "10.100.0.2",
		InsecureTLS:  true,
	}); err != nil {
		t.Fatal(err)
	}
	if err := Peers(); err != nil {
		t.Fatalf("Peers: %v", err)
	}
}

func TestPeersServerUnreachable(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := SaveClientConfig(&ClientConfig{
		ServerDomain: "127.0.0.1:1",
		InsecureTLS:  true,
	}); err != nil {
		t.Fatal(err)
	}
	if err := Peers(); err == nil {
		t.Fatal("expected error when server unreachable")
	}
}

func TestWriteStatus(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "status.json")

	// Zero-value client: connectedAt/lastActive unset → both time branches
	// take the ns<=0 path (zero time).
	tc := &TunnelClient{}
	tc.writeStatus(path)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected status file written: %v", err)
	}

	// Now populate the atomics so the ns>0 branches are exercised.
	tc.connected.Store(1)
	tc.connectedAt.Store(time.Now().UnixNano())
	tc.lastActive.Store(time.Now().UnixNano())
	tc.txPackets.Store(10)
	tc.txBytes.Store(2048)
	tc.rxPackets.Store(8)
	tc.rxBytes.Store(1024)
	tc.lastRTT.Store(500)
	tc.writeStatus(path)

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read status: %v", err)
	}
	var s ClientStats
	if err := json.Unmarshal(data, &s); err != nil {
		t.Fatalf("unmarshal status: %v", err)
	}
	if !s.Connected || s.TxPackets != 10 || s.RxBytes != 1024 {
		t.Fatalf("unexpected status content: %+v", s)
	}
	if s.ConnectedAt.IsZero() || s.LastActive.IsZero() {
		t.Fatal("expected ConnectedAt/LastActive to be populated")
	}
}

// TestWriteStatusRenameFailure 让目标路径本身是一个目录，使 os.Rename
// 失败，验证 writeStatus 不 panic 且清理掉临时文件（不残留 .tmp）。
func TestWriteStatusRenameFailure(t *testing.T) {
	dir := t.TempDir()
	// path 指向一个已存在的目录：rename(tmpfile -> dir) 会失败。
	path := filepath.Join(dir, "statusdir")
	if err := os.Mkdir(path, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	tc := &TunnelClient{}
	tc.writeStatus(path) // 不应 panic

	// 临时文件应被清理。
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Fatalf("expected temp file to be cleaned up, stat err=%v", err)
	}
}
