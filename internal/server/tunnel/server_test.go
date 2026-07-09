package tunnel

import (
	"net/netip"
	"testing"
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
}

func TestRoutePacketMalformed(t *testing.T) {
	ts := &TunnelServer{router: NewRouter()}
	// Too-short packet: ExtractDstIP errors, routePacket returns silently.
	ts.routePacket([]byte{0x45, 0x00})
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
