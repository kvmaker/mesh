package device

import (
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
