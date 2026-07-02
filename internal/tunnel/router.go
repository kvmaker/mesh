package tunnel

import (
	"net/netip"
	"sync"

	"github.com/coder/websocket"
)

// ClientConn represents a client connection with its associated metadata.
type ClientConn struct {
	Conn     *websocket.Conn
	DeviceID string
	IP       netip.Addr
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
