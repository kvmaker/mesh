package tunnel

import (
	"context"
	"log"
	"net/netip"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
)

// ClientConn represents a client connection with its associated metadata.
//
// Since TODO P02 the connection is the single writer to its WebSocket:
// forwarding paths only Enqueue packets and a writeLoop goroutine drains
// the queue serially. This avoids both concurrent WebSocket writes and
// the head-of-line blocking caused by slow peers on the read side.
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

	// SendQueue buffers packets waiting to be written to the WebSocket.
	// It is owned by writeLoop.
	SendQueue chan Packet
	// Done is closed by Close to signal writeLoop to exit.
	Done chan struct{}
	// writeLoopDone is closed when writeLoop returns; useful for tests
	// to wait for the goroutine to actually exit.
	writeLoopDone chan struct{}

	// DropPackets counts packets dropped because the send queue was full.
	DropPackets atomic.Uint64
	// QueueDepth is the current number of packets buffered in SendQueue.
	QueueDepth atomic.Int64
	// QueueMaxDepth records the high-water mark of QueueDepth since the
	// connection was created.
	QueueMaxDepth atomic.Int64

	closed atomic.Bool
}

// NewClientConn wraps an accepted WebSocket into a ClientConn and starts its
// single-writer goroutine. Forwarding paths must call Enqueue instead of
// writing to Conn directly.
func NewClientConn(conn *websocket.Conn, deviceID string, ip netip.Addr, queueSize int) *ClientConn {
	if queueSize <= 0 {
		queueSize = DefaultSendQueueSize
	}
	cc := &ClientConn{
		Conn:          conn,
		DeviceID:      deviceID,
		IP:            ip,
		ConnectedAt:   time.Now(),
		SendQueue:     make(chan Packet, queueSize),
		Done:          make(chan struct{}),
		writeLoopDone: make(chan struct{}),
	}
	go cc.writeLoop()
	return cc
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

// Enqueue submits a packet to the per-connection send queue. When the queue
// is full it drops the packet (drop_tail policy) and increments DropPackets.
// Returns true if the packet was accepted, false if dropped.
//
// Callers must not mutate pkt.Data after handing it to Enqueue.
func (cc *ClientConn) Enqueue(pkt Packet) bool {
	// Pre-check closed so we never enqueue after Close has flipped.
	// The select below also re-checks closed so a concurrent Close cannot
	// strand a packet in SendQueue once it has been closed.
	if cc.closed.Load() {
		cc.DropPackets.Add(1)
		return false
	}
	// Account for the packet before sending. This avoids a window where
	// writeLoop has already decremented QueueDepth for a previous packet
	// while the new one has not yet been observed.
	depth := cc.QueueDepth.Add(1)
	for {
		cur := cc.QueueMaxDepth.Load()
		if depth <= cur || cc.QueueMaxDepth.CompareAndSwap(cur, depth) {
			break
		}
	}
	select {
	case cc.SendQueue <- pkt:
		return true
	default:
		// Roll back the depth bump and count it as dropped.
		cc.QueueDepth.Add(-1)
		cc.DropPackets.Add(1)
		return false
	case <-cc.Done:
		// Connection has been closed while we were trying to enqueue.
		// Roll back the depth bump; writeLoop will not consume this slot.
		cc.QueueDepth.Add(-1)
		cc.DropPackets.Add(1)
		return false
	}
}

// Close stops the writeLoop and releases the send queue. Safe to call
// multiple times.
func (cc *ClientConn) Close() {
	if cc.closed.Swap(true) {
		return
	}
	close(cc.Done)
}

// writeLoop is the single writer to the underlying WebSocket for this
// ClientConn. It exits when Done is closed or when the connection is nil.
func (cc *ClientConn) writeLoop() {
	defer close(cc.writeLoopDone)
	if cc.Conn == nil {
		// Defensive: tests and any future misuse that passes a nil conn
		// should not crash the process.
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		<-cc.Done
		cancel()
	}()

	for {
		select {
		case <-cc.Done:
			// Drain remaining packets so a fast producer does not leak
			// them silently. Use a fresh, bounded context here: the
			// normal ctx is already canceled at this point, which would
			// cause every drain write to fail with context.Canceled.
			drainCtx, drainCancel := context.WithTimeout(context.Background(), 2*time.Second)
			for {
				select {
				case pkt := <-cc.SendQueue:
					cc.QueueDepth.Add(-1)
					if err := cc.Conn.Write(drainCtx, websocket.MessageBinary, pkt.Data); err != nil {
						log.Printf("write to client %s during shutdown: %v", cc.DeviceID, err)
					} else {
						cc.RecordTx(len(pkt.Data))
					}
				default:
					drainCancel()
					return
				}
			}
		case pkt := <-cc.SendQueue:
			cc.QueueDepth.Add(-1)
			if err := cc.Conn.Write(ctx, websocket.MessageBinary, pkt.Data); err != nil {
				log.Printf("write to client %s: %v", cc.DeviceID, err)
				// Stop the loop on the first write error: the connection
				// is likely broken and the reader side will surface it.
				return
			}
			cc.RecordTx(len(pkt.Data))
		}
	}
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
	DropPackets uint64    `json:"drop_packets"`
	QueueDepth  int64     `json:"queue_depth"`
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
			DropPackets: cc.DropPackets.Load(),
			QueueDepth:  cc.QueueDepth.Load(),
			LastPacket:  lastPkt,
		})
	}
	return stats
}
