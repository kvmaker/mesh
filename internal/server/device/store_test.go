package device

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maxyu/mesh/internal/server/db"
)

func setupDB(t *testing.T) *sql.DB {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	if err := db.Migrate(d); err != nil {
		d.Close()
		t.Fatalf("db.Migrate: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

func TestCreateAndGet(t *testing.T) {
	d := setupDB(t)
	dev := &Device{ID: "id1", Name: "test", IP: "10.100.0.2", Secret: "sec1"}
	if err := Create(d, dev); err != nil {
		t.Fatal(err)
	}
	got, err := GetByID(d, "id1")
	if err != nil {
		t.Fatal(err)
	}
	if got.IP != "10.100.0.2" {
		t.Fatalf("expected 10.100.0.2, got %s", got.IP)
	}
}

func TestGetBySecret(t *testing.T) {
	d := setupDB(t)
	Create(d, &Device{ID: "id1", Name: "test", IP: "10.100.0.2", Secret: "mysecret"})
	got, err := GetBySecret(d, "mysecret")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "id1" {
		t.Fatalf("expected id1, got %s", got.ID)
	}
}

func TestGetByIDNotFound(t *testing.T) {
	d := setupDB(t)
	if _, err := GetByID(d, "missing"); err == nil {
		t.Fatal("expected error for missing device")
	}
}

func TestGetBySecretNotFound(t *testing.T) {
	d := setupDB(t)
	if _, err := GetBySecret(d, "nope"); err == nil {
		t.Fatal("expected error for missing secret")
	}
}

func TestListEmpty(t *testing.T) {
	d := setupDB(t)
	devs, err := List(d)
	if err != nil {
		t.Fatal(err)
	}
	if len(devs) != 0 {
		t.Fatalf("expected empty list, got %d", len(devs))
	}
}

func TestListMultiple(t *testing.T) {
	d := setupDB(t)
	Create(d, &Device{ID: "a", Name: "na", IP: "10.100.0.2", Secret: "sa"})
	Create(d, &Device{ID: "b", Name: "nb", IP: "10.100.0.3", Secret: "sb"})
	devs, err := List(d)
	if err != nil {
		t.Fatal(err)
	}
	if len(devs) != 2 {
		t.Fatalf("expected 2 devices, got %d", len(devs))
	}
	ids := map[string]bool{}
	for _, dev := range devs {
		ids[dev.ID] = true
	}
	if !ids["a"] || !ids["b"] {
		t.Fatalf("missing expected ids, got %v", ids)
	}
}

// TestLastSeenPopulated exercises the lastSeen.Valid branch in
// GetBySecret and List once a device has reported in via UpdateOnline.
func TestLastSeenPopulated(t *testing.T) {
	d := setupDB(t)
	Create(d, &Device{ID: "id1", Name: "test", IP: "10.100.0.2", Secret: "sec1"})
	if err := UpdateOnline(d, "id1", true); err != nil {
		t.Fatal(err)
	}

	bySecret, err := GetBySecret(d, "sec1")
	if err != nil {
		t.Fatal(err)
	}
	if bySecret.LastSeen.IsZero() {
		t.Fatal("expected GetBySecret to populate LastSeen")
	}

	devs, err := List(d)
	if err != nil {
		t.Fatal(err)
	}
	if len(devs) != 1 || devs[0].LastSeen.IsZero() {
		t.Fatalf("expected List to populate LastSeen, got %+v", devs)
	}
}

func TestDelete(t *testing.T) {
	d := setupDB(t)
	Create(d, &Device{ID: "id1", Name: "test", IP: "10.100.0.2", Secret: "sec1"})
	if err := Delete(d, "id1"); err != nil {
		t.Fatal(err)
	}
	if _, err := GetByID(d, "id1"); err == nil {
		t.Fatal("expected device to be deleted")
	}
}

func TestDeleteNotFound(t *testing.T) {
	d := setupDB(t)
	if err := Delete(d, "ghost"); err == nil {
		t.Fatal("expected not found error")
	}
}

func TestUpdateOnline(t *testing.T) {
	d := setupDB(t)
	Create(d, &Device{ID: "id1", Name: "test", IP: "10.100.0.2", Secret: "sec1"})
	if err := UpdateOnline(d, "id1", true); err != nil {
		t.Fatal(err)
	}
	got, err := GetByID(d, "id1")
	if err != nil {
		t.Fatal(err)
	}
	if !got.Online {
		t.Fatal("expected device to be online")
	}
	if got.LastSeen.IsZero() {
		t.Fatal("expected last_seen to be set")
	}
	if err := UpdateOnline(d, "id1", false); err != nil {
		t.Fatal(err)
	}
	got, _ = GetByID(d, "id1")
	if got.Online {
		t.Fatal("expected device to be offline")
	}
}

func TestAllocateAndCreateSuccess(t *testing.T) {
	d := setupDB(t)
	dev, err := AllocateAndCreate(d, "10.100.0.0/24", "id1", "host1", "sec1")
	if err != nil {
		t.Fatalf("AllocateAndCreate: %v", err)
	}
	// 首个可用主机应为 .2（.1 保留给服务端）。
	if dev.IP != "10.100.0.2" {
		t.Fatalf("expected 10.100.0.2, got %s", dev.IP)
	}
	got, err := GetByID(d, "id1")
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.IP != dev.IP {
		t.Fatalf("persisted IP %s != returned %s", got.IP, dev.IP)
	}
}

// TestAllocateAndCreateSequential 连续分配应拿到递增的不同 IP，验证
// 前一次 Create 提交后 Allocate 能看到已占用集合、不会重复分配。
func TestAllocateAndCreateSequential(t *testing.T) {
	d := setupDB(t)
	seen := map[string]bool{}
	for i := 0; i < 3; i++ {
		dev, err := AllocateAndCreate(d, "10.100.0.0/24", fmt.Sprintf("id%d", i), "h", fmt.Sprintf("sec%d", i))
		if err != nil {
			t.Fatalf("iteration %d: %v", i, err)
		}
		if seen[dev.IP] {
			t.Fatalf("duplicate IP allocated: %s", dev.IP)
		}
		seen[dev.IP] = true
	}
}

// TestAllocateAndCreateDuplicateIDReturnsImmediately 验证重复 device ID
// 触发 devices.id 冲突时立即返回原始错误，而不是当成 IP 竞争重试到耗尽。
func TestAllocateAndCreateDuplicateIDReturnsImmediately(t *testing.T) {
	d := setupDB(t)
	if _, err := AllocateAndCreate(d, "10.100.0.0/24", "dup", "h", "s1"); err != nil {
		t.Fatalf("first create: %v", err)
	}
	// 相同 ID 再次创建：Create 因 devices.id 主键/唯一冲突失败，
	// 应立即返回该错误，错误消息里应体现 id 冲突而非误导性的
	// "allocate unique IP after N attempts"。
	_, err := AllocateAndCreate(d, "10.100.0.0/24", "dup", "h", "s2")
	if err == nil {
		t.Fatal("expected error when reusing an existing device ID")
	}
	if strings.Contains(err.Error(), "allocate unique IP after") {
		t.Fatalf("id conflict should not be reported as IP-allocation exhaustion: %v", err)
	}
	if !strings.Contains(err.Error(), "devices.id") {
		t.Fatalf("expected devices.id conflict in error, got: %v", err)
	}
}

// TestAllocateAndCreateRetriesOnIPConflict 验证当预先占用了首个可用 IP
// （模拟并发抢占）时，AllocateAndCreate 会重试分配到下一个空闲 IP 而非失败。
func TestAllocateAndCreateRetriesOnIPConflict(t *testing.T) {
	d := setupDB(t)
	// 预占 .2（Allocate 会先选它），迫使首次 Create 命中 devices.ip 冲突，
	// 重试后拿到 .3。
	if err := Create(d, &Device{ID: "squatter", Name: "h", IP: "10.100.0.2", Secret: "sq"}); err != nil {
		t.Fatalf("seed squatter: %v", err)
	}
	dev, err := AllocateAndCreate(d, "10.100.0.0/24", "id1", "h", "sec1")
	if err != nil {
		t.Fatalf("AllocateAndCreate should retry past occupied IP: %v", err)
	}
	if dev.IP != "10.100.0.3" {
		t.Fatalf("expected retry to allocate 10.100.0.3, got %s", dev.IP)
	}
}

// TestAllocateAndCreateInvalidNetwork 验证 Allocate 出错时立即返回。
func TestAllocateAndCreateInvalidNetwork(t *testing.T) {
	d := setupDB(t)
	if _, err := AllocateAndCreate(d, "not-a-cidr", "id1", "h", "s"); err == nil {
		t.Fatal("expected error for invalid network CIDR")
	}
}
