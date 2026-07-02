package db

import (
	"path/filepath"
	"testing"
)

func TestOpenAndMigrate(t *testing.T) {
	d, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if err := Migrate(d); err != nil {
		t.Fatal(err)
	}
	var name string
	d.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name='devices'").Scan(&name)
	if name != "devices" {
		t.Fatalf("expected devices table, got %q", name)
	}
}
