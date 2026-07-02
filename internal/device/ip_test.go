package device

import (
	"database/sql"
	"fmt"
	"testing"

	"github.com/maxyu/mesh/internal/db"
)

func setupDB(t *testing.T) *sql.DB {
	t.Helper()
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.Migrate(database); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	return database
}

func TestAllocateFirstIP(t *testing.T) {
	database := setupDB(t)
	ip, err := Allocate(database, "10.100.0.0/24")
	if err != nil {
		t.Fatalf("Allocate failed: %v", err)
	}
	if ip != "10.100.0.2" {
		t.Fatalf("expected 10.100.0.2, got %s", ip)
	}
}

func TestAllocateSequential(t *testing.T) {
	database := setupDB(t)
	ip1, _ := Allocate(database, "10.100.0.0/24")
	ip2, _ := Allocate(database, "10.100.0.0/24")
	if ip1 != "10.100.0.2" || ip2 != "10.100.0.3" {
		t.Fatalf("expected .2 and .3, got %s and %s", ip1, ip2)
	}
}

func TestAllocateSkipsUsedIPs(t *testing.T) {
	database := setupDB(t)
	// 手动插入一个占用 .2 的设备
	database.Exec("INSERT INTO devices (id, name, public_key, ip, secret, passive) VALUES ('x', 'x', 'x', '10.100.0.2', 'x', 0)")
	ip, _ := Allocate(database, "10.100.0.0/24")
	if ip != "10.100.0.3" {
		t.Fatalf("expected 10.100.0.3, got %s", ip)
	}
}

func TestAllocateExhausted(t *testing.T) {
	database := setupDB(t)
	// 填满整个子网 (.2 ~ .254 = 253 个)
	for i := 2; i <= 254; i++ {
		ip := fmt.Sprintf("10.100.0.%d", i)
		database.Exec("INSERT INTO devices (id, name, public_key, ip, secret, passive) VALUES (?, ?, ?, ?, 'x', 0)",
			fmt.Sprintf("id%d", i), fmt.Sprintf("n%d", i), fmt.Sprintf("pk%d", i), ip)
	}
	_, err := Allocate(database, "10.100.0.0/24")
	if err == nil {
		t.Fatal("expected error when IPs exhausted")
	}
}
