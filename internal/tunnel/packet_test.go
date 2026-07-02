package tunnel

import (
	"net/netip"
	"testing"
)

func TestExtractDstIPv4(t *testing.T) {
	// Construct minimal IPv4 packet header: version=4, IHL=5, dst=10.100.0.5
	pkt := make([]byte, 20)
	pkt[0] = 0x45 // version 4, IHL 5
	pkt[16], pkt[17], pkt[18], pkt[19] = 10, 100, 0, 5

	dst, err := ExtractDstIP(pkt)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := netip.MustParseAddr("10.100.0.5")
	if dst != expected {
		t.Fatalf("expected %s, got %s", expected, dst)
	}
}

func TestExtractDstIPv4Multiple(t *testing.T) {
	tests := []struct {
		name      string
		octets    [4]byte
		expected  string
	}{
		{"192.168.1.1", [4]byte{192, 168, 1, 1}, "192.168.1.1"},
		{"127.0.0.1", [4]byte{127, 0, 0, 1}, "127.0.0.1"},
		{"8.8.8.8", [4]byte{8, 8, 8, 8}, "8.8.8.8"},
		{"255.255.255.255", [4]byte{255, 255, 255, 255}, "255.255.255.255"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pkt := make([]byte, 20)
			pkt[0] = 0x45
			copy(pkt[16:20], tt.octets[:])

			dst, err := ExtractDstIP(pkt)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			expected := netip.MustParseAddr(tt.expected)
			if dst != expected {
				t.Fatalf("expected %s, got %s", expected, dst)
			}
		})
	}
}

func TestExtractDstIPv6(t *testing.T) {
	// Construct minimal IPv6 packet header: version=6, dst=2001:db8::1
	pkt := make([]byte, 40)
	pkt[0] = 0x60 // version 6, traffic class high 4 bits

	// Destination IPv6: 2001:db8::1 = [0x20, 0x01, 0x0d, 0xb8, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1]
	dstBytes := []byte{0x20, 0x01, 0x0d, 0xb8, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1}
	copy(pkt[24:40], dstBytes)

	dst, err := ExtractDstIP(pkt)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := netip.MustParseAddr("2001:db8::1")
	if dst != expected {
		t.Fatalf("expected %s, got %s", expected, dst)
	}
}

func TestExtractDstIPTooShort(t *testing.T) {
	_, err := ExtractDstIP([]byte{0x45, 0x00})
	if err == nil {
		t.Fatal("expected error for short packet")
	}
}

func TestExtractDstIPv6TooShort(t *testing.T) {
	// IPv6 header requires 40 bytes minimum
	pkt := make([]byte, 39)
	pkt[0] = 0x60 // version 6

	_, err := ExtractDstIP(pkt)
	if err == nil {
		t.Fatal("expected error for short IPv6 packet")
	}
}

func TestExtractDstIPUnknownVersion(t *testing.T) {
	pkt := make([]byte, 20)
	pkt[0] = 0x55 // version 5 (invalid)

	_, err := ExtractDstIP(pkt)
	if err == nil {
		t.Fatal("expected error for unknown IP version")
	}
}
