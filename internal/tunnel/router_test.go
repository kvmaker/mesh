package tunnel

import (
	"net/netip"
	"sync"
	"testing"
	"time"
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
	ip := netip.MustParseAddr("10.100.0.2")
	cc := NewClientConn(nil, "dev1", ip, 1)

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
	ip := netip.MustParseAddr("10.100.0.2")
	cc := NewClientConn(nil, "dev1", ip, 1)
	cc.Close()
	cc.Close() // must not panic
	select {
	case <-cc.writeLoopDone:
	case <-time.After(2 * time.Second):
		t.Fatal("writeLoop did not exit")
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
