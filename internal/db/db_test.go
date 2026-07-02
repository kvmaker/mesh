package db

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOpenAndMigrate(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	database, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer database.Close()

	err = Migrate(database)
	if err != nil {
		t.Fatalf("Migrate failed: %v", err)
	}

	// 验证 devices 表存在
	var name string
	err = database.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name='devices'").Scan(&name)
	if err != nil {
		t.Fatalf("devices table not found: %v", err)
	}
	if name != "devices" {
		t.Fatalf("expected 'devices', got %q", name)
	}
}

func TestOpenCreatesFile(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "subdir", "test.db")

	database, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	database.Close()

	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Fatal("database file was not created")
	}
}
