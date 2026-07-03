package device

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/maxyu/mesh/internal/db"
)

func setupDB(t *testing.T) *sql.DB {
	t.Helper()
	d, _ := db.Open(filepath.Join(t.TempDir(), "test.db"))
	db.Migrate(d)
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
