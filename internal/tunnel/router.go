package tunnel

import (
	"net/netip"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
)

// ClientConn represents a client connection with its associated metadata.
type ClientConn struct {
	Conn        *websocket.Conn
	DeviceID    string
	IP          netip.Addr
	ConnectedAt time.Time

	TxPackets      atomic.Uint64
	TxBytes        atomic.Uint64
	RxPackets      atomic.Uint64
	RxBytes        atomic.Uint64
	LastPacketTime atomic.Int64 // unix nano
}

// RecordTx records an outgoing packet (server → client).
func (cc *ClientConn) RecordTx(size int) {
	cc.TxPackets.Add(1)
	cc.TxBytes.Add(uint64(size))
	cc.LastPacketTime.Store(time.Now().UnixNano())
}

// RecordRx records an incoming packet (client → server).
func (cc *ClientConn) RecordRx(size int) {
	cc.RxPackets.Add(1)
	cc.RxBytes.Add(uint64(size))
	cc.LastPacketTime.Store(time.Now().UnixNano())
}

// ConnStats is a point-in-time snapshot of a connected client's statistics.
type ConnStats struct {
	DeviceID    string    `json:"device_id"`
	IP          string    `json:"ip"`
	ConnectedAt time.Time `json:"connected_at"`
	TxPackets   uint64    `json:"tx_packets"`
	TxBytes     uint64    `json:"tx_bytes"`
	RxPackets   uint64    `json:"rx_packets"`
	RxBytes     uint64    `json:"rx_bytes"`
	LastPacket  time.Time `json:"last_packet,omitzero"`
}

// Router is a concurrent-safe routing table that maps destination IPs to ClientConns.
type Router struct {
	mu     sync.RWMutex
	routes map[netip.Addr]*ClientConn
}

// NewRouter creates and returns a new Router instance.
func NewRouter() *Router {
	return &Router{
		routes: make(map[netip.Addr]*ClientConn),
	}
}

// Register adds a route entry mapping the given IP to a ClientConn.
func (r *Router) Register(ip netip.Addr, cc *ClientConn) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.routes[ip] = cc
}

// Unregister removes a route entry for the given IP.
func (r *Router) Unregister(ip netip.Addr) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.routes, ip)
}

// Lookup retrieves the ClientConn for a given destination IP.
// Returns (cc, true) if found, (nil, false) otherwise.
func (r *Router) Lookup(dst netip.Addr) (*ClientConn, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	cc, ok := r.routes[dst]
	return cc, ok
}

// Stats returns a snapshot of all connected clients' statistics.
func (r *Router) Stats() []ConnStats {
	r.mu.RLock()
	defer r.mu.RUnlock()
	stats := make([]ConnStats, 0, len(r.routes))
	for _, cc := range r.routes {
		var lastPkt time.Time
		if ns := cc.LastPacketTime.Load(); ns > 0 {
			lastPkt = time.Unix(0, ns)
		}
		stats = append(stats, ConnStats{
			DeviceID:    cc.DeviceID,
			IP:          cc.IP.String(),
			ConnectedAt: cc.ConnectedAt,
			TxPackets:   cc.TxPackets.Load(),
			TxBytes:     cc.TxBytes.Load(),
			RxPackets:   cc.RxPackets.Load(),
			RxBytes:     cc.RxBytes.Load(),
			LastPacket:  lastPkt,
		})
	}
	return stats
}
