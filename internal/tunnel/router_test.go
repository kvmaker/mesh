package tunnel

import (
	"net/netip"
	"sync"
	"testing"
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
