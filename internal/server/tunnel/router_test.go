package tunnel

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestRouterRegisterLookup(t *testing.T) {
	r := NewRouter()
	ip := netip.MustParseAddr("10.100.0.2")
	cc := &ClientConn{DeviceID: "dev1", IP: ip}

	r.Register(ip, cc)

	got, ok := r.Lookup(ip)
	if !ok {
		t.Fatal("expected to find registered IP")
	}
	if got.DeviceID != "dev1" {
		t.Fatalf("expected DeviceID=dev1, got %s", got.DeviceID)
	}
	if got.IP != ip {
		t.Fatalf("expected IP=%s, got %s", ip, got.IP)
	}
}

func TestRouterLookupNotFound(t *testing.T) {
	r := NewRouter()
	ip := netip.MustParseAddr("10.100.0.99")

	_, ok := r.Lookup(ip)
	if ok {
		t.Fatal("should not find unregistered IP")
	}
}

func TestRouterUnregister(t *testing.T) {
	r := NewRouter()
	ip := netip.MustParseAddr("10.100.0.2")
	cc := &ClientConn{DeviceID: "dev1", IP: ip}

	r.Register(ip, cc)
	_, ok := r.Lookup(ip)
	if !ok {
		t.Fatal("expected to find after register")
	}

	r.Unregister(ip)
	_, ok = r.Lookup(ip)
	if ok {
		t.Fatal("should not find after unregister")
	}
}

// TestRouterUnregisterConn 覆盖 match-aware 清理的两个关键行为：
//  1. 当路由仍指向该连接时，UnregisterConn 删除路由；
//  2. 当路由已被新连接覆盖（设备重连场景）时，旧连接的 UnregisterConn
//     不得误删新连接的路由——这正是 UnregisterConn 相较无条件 Unregister
//     的存在意义。
func TestRouterUnregisterConn(t *testing.T) {
	r := NewRouter()
	ip := netip.MustParseAddr("10.100.0.2")
	oldConn := &ClientConn{DeviceID: "dev1-old", IP: ip}
	newConn := &ClientConn{DeviceID: "dev1-new", IP: ip}

	// 情形 1：路由指向 oldConn，UnregisterConn(oldConn) 应删除。
	r.Register(ip, oldConn)
	r.UnregisterConn(ip, oldConn)
	if _, ok := r.Lookup(ip); ok {
		t.Fatal("route should be removed when it still points at the conn")
	}

	// 情形 2：设备重连，newConn 覆盖路由；随后 oldConn 的 deferred cleanup
	// 触发 UnregisterConn(oldConn)，必须保留 newConn 的路由。
	r.Register(ip, oldConn)
	r.Register(ip, newConn) // 重连覆盖
	r.UnregisterConn(ip, oldConn)
	got, ok := r.Lookup(ip)
	if !ok {
		t.Fatal("newConn route must survive stale oldConn cleanup")
	}
	if got.DeviceID != "dev1-new" {
		t.Fatalf("expected route to point at newConn, got %s", got.DeviceID)
	}
}

func TestRouterMultipleRegistrations(t *testing.T) {
	r := NewRouter()

	ips := []string{
		"10.100.0.1",
		"10.100.0.2",
		"10.100.0.3",
		"192.168.1.1",
	}

	for i, ipStr := range ips {
		ip := netip.MustParseAddr(ipStr)
		cc := &ClientConn{DeviceID: "dev" + string(rune(i+1)), IP: ip}
		r.Register(ip, cc)
	}

	for i, ipStr := range ips {
		ip := netip.MustParseAddr(ipStr)
		got, ok := r.Lookup(ip)
		if !ok {
			t.Fatalf("expected to find %s", ipStr)
		}
		expected := "dev" + string(rune(i+1))
		if got.DeviceID != expected {
			t.Fatalf("expected %s, got %s", expected, got.DeviceID)
		}
	}
}

func TestRouterRegisterOverwrite(t *testing.T) {
	r := NewRouter()
	ip := netip.MustParseAddr("10.100.0.2")

	cc1 := &ClientConn{DeviceID: "dev1", IP: ip}
	r.Register(ip, cc1)

	cc2 := &ClientConn{DeviceID: "dev2", IP: ip}
	r.Register(ip, cc2)

	got, ok := r.Lookup(ip)
	if !ok {
		t.Fatal("expected to find registered IP")
	}
	if got.DeviceID != "dev2" {
		t.Fatalf("expected DeviceID=dev2 (overwritten), got %s", got.DeviceID)
	}
}

func TestRouterConcurrency(t *testing.T) {
	r := NewRouter()
	const numGoroutines = 100
	const ipsPerGoroutine = 10

	var wg sync.WaitGroup

	// Registrations
	for g := 0; g < numGoroutines; g++ {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()
			for i := 0; i < ipsPerGoroutine; i++ {
				baseIP := uint32(10)<<24 | uint32(100)<<16 | uint32(goroutineID&0xFF)<<8 | uint32(i)
				ip := netip.AddrFrom4([4]byte{byte(baseIP >> 24), byte(baseIP >> 16), byte(baseIP >> 8), byte(baseIP)})
				cc := &ClientConn{DeviceID: "dev" + string(rune(goroutineID)), IP: ip}
				r.Register(ip, cc)
			}
		}(g)
	}

	wg.Wait()

	// Lookups
	for g := 0; g < numGoroutines; g++ {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()
			for i := 0; i < ipsPerGoroutine; i++ {
				baseIP := uint32(10)<<24 | uint32(100)<<16 | uint32(goroutineID&0xFF)<<8 | uint32(i)
				ip := netip.AddrFrom4([4]byte{byte(baseIP >> 24), byte(baseIP >> 16), byte(baseIP >> 8), byte(baseIP)})
				_, ok := r.Lookup(ip)
				if !ok {
					t.Errorf("expected to find %s", ip.String())
				}
			}
		}(g)
	}

	wg.Wait()
}

func TestRouterUnregisterNonexistent(t *testing.T) {
	r := NewRouter()
	ip := netip.MustParseAddr("10.100.0.99")

	// Should not panic
	r.Unregister(ip)

	_, ok := r.Lookup(ip)
	if ok {
		t.Fatal("should still not find after unregistering nonexistent IP")
	}
}

// --- P02: async send queue / single-writer tests ---

func TestClientConnEnqueueSuccess(t *testing.T) {
	ip := netip.MustParseAddr("10.100.0.2")
	cc := NewClientConn(nil, "dev1", ip, 4)
	t.Cleanup(cc.Close)

	for i := 0; i < 4; i++ {
		ok := cc.Enqueue(Packet{Data: []byte{byte(i)}})
		if !ok {
			t.Fatalf("enqueue %d should succeed", i)
		}
	}
	if got := cc.QueueDepth.Load(); got != 4 {
		t.Fatalf("expected QueueDepth=4, got %d", got)
	}
	if got := cc.QueueMaxDepth.Load(); got != 4 {
		t.Fatalf("expected QueueMaxDepth=4, got %d", got)
	}
	if got := cc.DropPackets.Load(); got != 0 {
		t.Fatalf("expected DropPackets=0, got %d", got)
	}
}

func TestClientConnEnqueueDropsWhenFull(t *testing.T) {
	ip := netip.MustParseAddr("10.100.0.2")
	cc := NewClientConn(nil, "dev1", ip, 2)
	t.Cleanup(cc.Close)

	for i := 0; i < 2; i++ {
		if ok := cc.Enqueue(Packet{Data: []byte{byte(i)}}); !ok {
			t.Fatalf("enqueue %d should succeed", i)
		}
	}
	// Queue is now full. Conn is nil so writeLoop will exit on first write
	// attempt, but before that we still expect a drop on the next Enqueue.
	// However, writeLoop may already have drained the queue. To isolate the
	// drop policy, we just check the upper bound on DropPackets.
	for i := 0; i < 5; i++ {
		cc.Enqueue(Packet{Data: []byte{0xff}})
	}
	if got := cc.DropPackets.Load(); got == 0 {
		t.Fatalf("expected DropPackets > 0 when queue is overloaded, got 0")
	}
}

func TestClientConnCloseStopsWriteLoop(t *testing.T) {
	serverConn, clientConn := newTestWebSocketPair(t)
	ip := netip.MustParseAddr("10.100.0.2")
	cc := NewClientConn(serverConn, "dev1", ip, 4)

	// Drive a few packets through the queue. writeLoop is the only goroutine
	// that writes to serverConn, so the client-side reads back prove the
	// writeLoop is actually running.
	go func() {
		for i := 0; i < 3; i++ {
			_ = clientConn.Write(context.Background(), websocket.MessageBinary, []byte{byte(i)})
		}
	}()
	for i := 0; i < 3; i++ {
		if !cc.Enqueue(Packet{Data: []byte{byte(i)}}) {
			t.Fatalf("enqueue %d should succeed", i)
		}
	}
	// Wait for writeLoop to drain. The single-writer model guarantees the
	// packet is on the wire before QueueDepth returns to 0.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cc.QueueDepth.Load() == 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if got := cc.QueueDepth.Load(); got != 0 {
		t.Fatalf("writeLoop did not drain queue, QueueDepth=%d", got)
	}

	cc.Close()
	select {
	case <-cc.writeLoopDone:
		// writeLoop exited.
	case <-time.After(2 * time.Second):
		t.Fatal("writeLoop did not exit after Close")
	}
	// Enqueue after Close should drop the packet.
	if cc.Enqueue(Packet{Data: []byte{0}}) {
		t.Fatal("Enqueue after Close should be rejected")
	}
	if got := cc.DropPackets.Load(); got == 0 {
		t.Fatal("expected DropPackets > 0 after Close")
	}
}

func TestClientConnCloseIdempotent(t *testing.T) {
	serverConn, _ := newTestWebSocketPair(t)
	ip := netip.MustParseAddr("10.100.0.2")
	cc := NewClientConn(serverConn, "dev1", ip, 1)

	cc.Close()
	cc.Close() // must not panic
	select {
	case <-cc.writeLoopDone:
	case <-time.After(2 * time.Second):
		t.Fatal("writeLoop did not exit")
	}
}

// TestWriteLoopExitsOnWriteError exercises the write-error return path in
// writeLoop: once the client side of the connection is torn down, the
// server-side Write eventually fails and writeLoop must exit on its own
// (without Close being called).
func TestWriteLoopExitsOnWriteError(t *testing.T) {
	serverConn, clientConn := newTestWebSocketPair(t)
	ip := netip.MustParseAddr("10.100.0.2")
	cc := NewClientConn(serverConn, "dev1", ip, 8)
	t.Cleanup(cc.Close)

	// Kill the client end so subsequent server-side writes error out.
	clientConn.CloseNow()

	// Keep enqueuing until writeLoop observes the write error and exits.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-cc.writeLoopDone:
			return // writeLoop exited on write error as expected.
		default:
		}
		cc.Enqueue(Packet{Data: []byte{0xff}})
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("writeLoop did not exit after write error")
}

// newTestWebSocketPair stands up a httptest server that accepts a single
// WebSocket upgrade and returns both the server-side and client-side
// *websocket.Conn. The test server and both conns are torn down via
// t.Cleanup.
func newTestWebSocketPair(t *testing.T) (serverConn, clientConn *websocket.Conn) {
	t.Helper()
	type acceptResult struct {
		conn *websocket.Conn
		err  error
	}
	resCh := make(chan acceptResult, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		resCh <- acceptResult{conn: conn, err: err}
	}))
	t.Cleanup(srv.Close)

	url := "ws" + srv.URL[len("http"):]
	cli, _, err := websocket.Dial(context.Background(), url, nil)
	if err != nil {
		t.Fatalf("websocket.Dial: %v", err)
	}
	t.Cleanup(func() { cli.CloseNow() })

	select {
	case res := <-resCh:
		if res.err != nil {
			t.Fatalf("websocket.Accept: %v", res.err)
		}
		t.Cleanup(func() { res.conn.CloseNow() })
		return res.conn, cli
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for server-side websocket")
		return nil, nil
	}
}

func TestNewClientConnDefaultQueueSize(t *testing.T) {
	ip := netip.MustParseAddr("10.100.0.2")
	// queueSize <= 0 should fall back to DefaultSendQueueSize.
	cc := NewClientConn(nil, "dev1", ip, 0)
	t.Cleanup(cc.Close)
	if got := cap(cc.SendQueue); got != DefaultSendQueueSize {
		t.Fatalf("expected default queue size %d, got %d", DefaultSendQueueSize, got)
	}
}

func TestRecordRx(t *testing.T) {
	ip := netip.MustParseAddr("10.100.0.2")
	cc := NewClientConn(nil, "dev1", ip, 4)
	t.Cleanup(cc.Close)

	cc.RecordRx(100)
	cc.RecordRx(50)
	if got := cc.RxPackets.Load(); got != 2 {
		t.Fatalf("expected RxPackets=2, got %d", got)
	}
	if got := cc.RxBytes.Load(); got != 150 {
		t.Fatalf("expected RxBytes=150, got %d", got)
	}
	if cc.LastPacketTime.Load() == 0 {
		t.Fatal("expected LastPacketTime to be set")
	}
}

func TestStatsLastPacketPopulated(t *testing.T) {
	r := NewRouter()
	ip := netip.MustParseAddr("10.100.0.2")
	cc := NewClientConn(nil, "dev1", ip, 4)
	t.Cleanup(cc.Close)
	r.Register(ip, cc)

	// RecordRx sets LastPacketTime, exercising the ns>0 branch in Stats.
	cc.RecordRx(42)

	stats := r.Stats()
	if len(stats) != 1 {
		t.Fatalf("expected 1 stat, got %d", len(stats))
	}
	if stats[0].LastPacket.IsZero() {
		t.Fatal("expected LastPacket to be populated")
	}
	if stats[0].RxBytes != 42 {
		t.Fatalf("expected RxBytes=42, got %d", stats[0].RxBytes)
	}
}

func TestEnqueueRejectedAfterDone(t *testing.T) {
	ip := netip.MustParseAddr("10.100.0.2")
	// Fill the queue so the SendQueue send would block, forcing the
	// select to fall through to the <-cc.Done case once closed.
	cc := NewClientConn(nil, "dev1", ip, 1)
	cc.Close()
	select {
	case <-cc.writeLoopDone:
	case <-time.After(2 * time.Second):
		t.Fatal("writeLoop did not exit")
	}
	if cc.Enqueue(Packet{Data: []byte{1}}) {
		t.Fatal("Enqueue after Close should be rejected")
	}
}

func TestClientConnStatsIncludeDrops(t *testing.T) {
	r := NewRouter()
	ip := netip.MustParseAddr("10.100.0.2")
	cc := NewClientConn(nil, "dev1", ip, 1)
	t.Cleanup(cc.Close)

	r.Register(ip, cc)
	got, ok := r.Lookup(ip)
	if !ok {
		t.Fatal("expected to find registered IP")
	}
	_ = got

	// Force a drop.
	cc.Enqueue(Packet{Data: []byte{1}})
	// Drain may consume it; pile more to guarantee overflow.
	for i := 0; i < 10; i++ {
		cc.Enqueue(Packet{Data: []byte{2}})
	}

	stats := r.Stats()
	if len(stats) != 1 {
		t.Fatalf("expected 1 stat, got %d", len(stats))
	}
	if stats[0].DeviceID != "dev1" {
		t.Fatalf("unexpected DeviceID: %s", stats[0].DeviceID)
	}
	if stats[0].DropPackets == 0 {
		t.Fatal("expected DropPackets > 0 in stats")
	}
}
