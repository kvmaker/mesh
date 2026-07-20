package tunnel

import (
	"database/sql"
	"net/netip"
	"os"
	"path/filepath"
	"testing"

	"github.com/maxyu/mesh/internal/server/config"
	"github.com/maxyu/mesh/internal/server/db"
)

func TestServerIP(t *testing.T) {
	tests := []struct {
		name    string
		network string
		want    string
		wantErr bool
	}{
		{name: "typical /24", network: "10.100.0.0/24", want: "10.100.0.1"},
		{name: "other subnet", network: "192.168.50.0/24", want: "192.168.50.1"},
		{name: "invalid cidr", network: "not-a-cidr", wantErr: true},
		{name: "ipv6 rejected", network: "fd00::/64", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := serverIP(tt.network)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error for %q", tt.network)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("serverIP(%q)=%q, want %q", tt.network, got, tt.want)
			}
		})
	}
}

// makeIPv4Packet builds a minimal 20-byte IPv4 header with the given
// destination address.
func makeIPv4Packet(dst netip.Addr) []byte {
	pkt := make([]byte, 20)
	pkt[0] = 0x45 // version 4, IHL 5
	b := dst.As4()
	pkt[16], pkt[17], pkt[18], pkt[19] = b[0], b[1], b[2], b[3]
	return pkt
}

func TestRoutePacket(t *testing.T) {
	dstIP := netip.MustParseAddr("10.100.0.2")
	cc := NewClientConn(nil, "dev1", dstIP, 4)
	t.Cleanup(cc.Close)

	ts := &TunnelServer{router: NewRouter()}
	ts.router.Register(dstIP, cc)

	ts.routePacket(makeIPv4Packet(dstIP))
	if got := cc.QueueDepth.Load(); got != 1 {
		t.Fatalf("expected packet to be enqueued, QueueDepth=%d", got)
	}
}

func TestRoutePacketNoRoute(t *testing.T) {
	ts := &TunnelServer{router: NewRouter()}
	// No panic and nothing enqueued for an unknown destination.
	ts.routePacket(makeIPv4Packet(netip.MustParseAddr("10.100.0.99")))
	if got := ts.routeMiss.Load(); got != 1 {
		t.Fatalf("expected routeMiss=1 after unknown destination, got %d", got)
	}
	if got := ts.parseErr.Load(); got != 0 {
		t.Fatalf("expected parseErr=0, got %d", got)
	}
}

func TestRoutePacketMalformed(t *testing.T) {
	ts := &TunnelServer{router: NewRouter()}
	// Too-short packet: ExtractDstIP errors, routePacket returns silently.
	ts.routePacket([]byte{0x45, 0x00})
	if got := ts.parseErr.Load(); got != 1 {
		t.Fatalf("expected parseErr=1 after malformed packet, got %d", got)
	}
	if got := ts.routeMiss.Load(); got != 0 {
		t.Fatalf("expected routeMiss=0, got %d", got)
	}
}

// TestDebugPacketDefaultOff guards the hot-path invariant that per-packet
// logging is disabled unless MESH_DEBUG_PACKET is explicitly set. If this
// flips to true by default it would reintroduce the P00 logging overhead.
func TestDebugPacketDefaultOff(t *testing.T) {
	if _, set := os.LookupEnv("MESH_DEBUG_PACKET"); set {
		t.Skip("MESH_DEBUG_PACKET is set in the environment")
	}
	if debugPacket {
		t.Fatal("debugPacket should default to false when MESH_DEBUG_PACKET is unset")
	}
}

func TestTunnelServerStats(t *testing.T) {
	ip := netip.MustParseAddr("10.100.0.2")
	cc := NewClientConn(nil, "dev1", ip, 4)
	t.Cleanup(cc.Close)

	ts := &TunnelServer{router: NewRouter()}
	ts.router.Register(ip, cc)

	stats := ts.Stats()
	if len(stats) != 1 {
		t.Fatalf("expected 1 stat, got %d", len(stats))
	}
	if stats[0].DeviceID != "dev1" {
		t.Fatalf("expected DeviceID=dev1, got %s", stats[0].DeviceID)
	}
}

// setupTestDB 建一个已 migrate 的临时 sqlite,供需要 *sql.DB 的测试使用。
func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	if err := db.Migrate(d); err != nil {
		t.Fatalf("db.Migrate: %v", err)
	}
	return d
}

// TestNewTunnelServerRelayNoTUN 验证 relay 模式不创建 TUN、不分配 tunName。
// CI 可跑:relay 路径不触碰 /dev/net/tun。
func TestNewTunnelServerRelayNoTUN(t *testing.T) {
	d := setupTestDB(t)
	cfg := config.Default()
	cfg.Mode = config.ModeRelay

	ts, err := NewTunnelServer(d, cfg)
	if err != nil {
		t.Fatalf("NewTunnelServer relay: %v", err)
	}
	defer ts.Close()

	if ts.tun != nil {
		t.Fatalf("relay mode must not create TUN, got non-nil tun")
	}
	if ts.tunName != "" {
		t.Fatalf("relay mode tunName must be empty, got %q", ts.tunName)
	}
}

// TestCloseRelayNilTUN 验证 relay 模式(ts.tun==nil)下 Close 不 panic。
func TestCloseRelayNilTUN(t *testing.T) {
	ts := &TunnelServer{router: NewRouter()} // tun==nil
	if err := ts.Close(); err != nil {
		t.Fatalf("Close on nil tun should be no-op, got %v", err)
	}
}

// TestRouteClientPacketRelayDropsServerBound 验证 relay 模式(tun==nil)下,
// 发给传统 server IP(10.100.0.1)的包被丢弃并计入 routeMiss。
func TestRouteClientPacketRelayDropsServerBound(t *testing.T) {
	ts := &TunnelServer{router: NewRouter()} // tun==nil ⇒ relay 语义
	ts.routeClientPacket(makeIPv4Packet(netip.MustParseAddr("10.100.0.1")))
	if got := ts.routeMiss.Load(); got != 1 {
		t.Fatalf("relay: expected routeMiss=1 for server-bound packet, got %d", got)
	}
}

// TestRouteClientPacketRelayForwardsToClient 验证 relay 模式下,
// 客户端间转发不受影响(对称于 TestRoutePacket)。
func TestRouteClientPacketRelayForwardsToClient(t *testing.T) {
	ts := &TunnelServer{router: NewRouter()} // tun==nil ⇒ relay 语义
	dstIP := netip.MustParseAddr("10.100.0.5")
	cc := NewClientConn(nil, "dev1", dstIP, 4)
	t.Cleanup(cc.Close)
	ts.router.Register(dstIP, cc)

	ts.routeClientPacket(makeIPv4Packet(dstIP))
	if got := cc.QueueDepth.Load(); got != 1 {
		t.Fatalf("relay: expected packet forwarded to client, QueueDepth=%d", got)
	}
}
