package device

import (
	"testing"
	"time"
)

func TestCreateAndGet(t *testing.T) {
	database := setupDB(t)
	d := &Device{
		ID:        "test-id",
		Name:      "macbook",
		PublicKey: "pubkey123",
		IP:        "10.100.0.2",
		Secret:    "secret123",
		Passive:   false,
	}

	if err := Create(database, d); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	got, err := GetByID(database, "test-id")
	if err != nil {
		t.Fatalf("GetByID failed: %v", err)
	}
	if got.Name != "macbook" || got.IP != "10.100.0.2" {
		t.Fatalf("unexpected device: %+v", got)
	}
}

func TestGetByPublicKey(t *testing.T) {
	database := setupDB(t)
	d := &Device{ID: "id1", Name: "dev1", PublicKey: "pk1", IP: "10.100.0.2", Secret: "s1"}
	Create(database, d)

	got, err := GetByPublicKey(database, "pk1")
	if err != nil {
		t.Fatalf("GetByPublicKey failed: %v", err)
	}
	if got.ID != "id1" {
		t.Fatalf("expected id1, got %s", got.ID)
	}
}

func TestList(t *testing.T) {
	database := setupDB(t)
	Create(database, &Device{ID: "1", Name: "a", PublicKey: "p1", IP: "10.100.0.2", Secret: "s1"})
	Create(database, &Device{ID: "2", Name: "b", PublicKey: "p2", IP: "10.100.0.3", Secret: "s2"})

	devices, err := List(database)
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(devices) != 2 {
		t.Fatalf("expected 2 devices, got %d", len(devices))
	}
}

func TestDelete(t *testing.T) {
	database := setupDB(t)
	Create(database, &Device{ID: "1", Name: "a", PublicKey: "p1", IP: "10.100.0.2", Secret: "s1"})

	if err := Delete(database, "1"); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	_, err := GetByID(database, "1")
	if err == nil {
		t.Fatal("expected error after delete")
	}
}

func TestUpdateHeartbeat(t *testing.T) {
	database := setupDB(t)
	Create(database, &Device{ID: "1", Name: "a", PublicKey: "p1", IP: "10.100.0.2", Secret: "s1"})

	if err := UpdateHeartbeat(database, "1"); err != nil {
		t.Fatalf("UpdateHeartbeat failed: %v", err)
	}

	d, _ := GetByID(database, "1")
	if !d.Online {
		t.Fatal("expected device to be online after heartbeat")
	}
	if d.LastSeen.IsZero() {
		t.Fatal("expected LastSeen to be set")
	}
}

func TestMarkOffline(t *testing.T) {
	database := setupDB(t)
	Create(database, &Device{ID: "1", Name: "a", PublicKey: "p1", IP: "10.100.0.2", Secret: "s1"})
	UpdateHeartbeat(database, "1")

	// 手动设置 last_seen 到 2 分钟前
	database.Exec("UPDATE devices SET last_seen = datetime('now', '-2 minutes') WHERE id = '1'")

	if err := MarkOffline(database, 90*time.Second); err != nil {
		t.Fatalf("MarkOffline failed: %v", err)
	}

	d, _ := GetByID(database, "1")
	if d.Online {
		t.Fatal("expected device to be offline")
	}
}
