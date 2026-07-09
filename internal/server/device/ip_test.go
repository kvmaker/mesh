package device

import (
	"fmt"
	"testing"
)

func TestAllocate(t *testing.T) {
	d := setupDB(t)
	ip, err := Allocate(d, "10.100.0.0/24")
	if err != nil {
		t.Fatal(err)
	}
	if ip != "10.100.0.2" {
		t.Fatalf("expected 10.100.0.2, got %s", ip)
	}
}

func TestAllocateSkipsUsed(t *testing.T) {
	d := setupDB(t)
	Create(d, &Device{ID: "x", Name: "x", IP: "10.100.0.2", Secret: "x"})
	ip, _ := Allocate(d, "10.100.0.0/24")
	if ip != "10.100.0.3" {
		t.Fatalf("expected 10.100.0.3, got %s", ip)
	}
}

func TestAllocateInvalidCIDR(t *testing.T) {
	d := setupDB(t)
	if _, err := Allocate(d, "not-a-cidr"); err == nil {
		t.Fatal("expected error for invalid CIDR")
	}
}

// TestAllocateIPv6Rejected 验证 IPv6 CIDR 被明确拒绝而非在 base[0] 处 panic。
func TestAllocateIPv6Rejected(t *testing.T) {
	d := setupDB(t)
	if _, err := Allocate(d, "fd00::/64"); err == nil {
		t.Fatal("expected error for IPv6 CIDR")
	}
}

func TestAllocateExhausted(t *testing.T) {
	d := setupDB(t)
	// Fill the entire usable range 2..254.
	for i := 2; i <= 254; i++ {
		ip := fmt.Sprintf("10.100.0.%d", i)
		if err := Create(d, &Device{
			ID:     fmt.Sprintf("id%d", i),
			Name:   fmt.Sprintf("n%d", i),
			IP:     ip,
			Secret: fmt.Sprintf("s%d", i),
		}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := Allocate(d, "10.100.0.0/24"); err == nil {
		t.Fatal("expected no-available-IPs error")
	}
}
