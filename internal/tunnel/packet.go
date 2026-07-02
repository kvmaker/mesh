package tunnel

import (
	"fmt"
	"net/netip"
)

// ExtractDstIP extracts the destination IP address from an IP packet header.
// Supports both IPv4 and IPv6 packets.
func ExtractDstIP(pkt []byte) (netip.Addr, error) {
	if len(pkt) < 20 {
		return netip.Addr{}, fmt.Errorf("packet too short: %d bytes", len(pkt))
	}

	version := pkt[0] >> 4
	switch version {
	case 4:
		// IPv4: destination IP is at bytes 16-19
		return netip.AddrFrom4([4]byte{pkt[16], pkt[17], pkt[18], pkt[19]}), nil
	case 6:
		// IPv6: destination IP is at bytes 24-39
		if len(pkt) < 40 {
			return netip.Addr{}, fmt.Errorf("IPv6 packet too short")
		}
		var addr [16]byte
		copy(addr[:], pkt[24:40])
		return netip.AddrFrom16(addr), nil
	default:
		return netip.Addr{}, fmt.Errorf("unknown IP version: %d", version)
	}
}
