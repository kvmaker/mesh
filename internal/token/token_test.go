package token

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/maxyu/mesh/internal/db"
)

func setup(t *testing.T) *sql.DB {
	t.Helper()
	d, _ := db.Open(filepath.Join(t.TempDir(), "test.db"))
	db.Migrate(d)
	t.Cleanup(func() { d.Close() })
	return d
}

func TestGenerateAndVerify(t *testing.T) {
	d := setup(t)
	tok, err := Generate()
	if err != nil {
		t.Fatal(err)
	}
	if len(tok) != 64 {
		t.Fatalf("expected 64 chars, got %d", len(tok))
	}
	Save(d, tok)
	if !Verify(d, tok) {
		t.Fatal("valid token should verify")
	}
	if Verify(d, "wrong") {
		t.Fatal("invalid token should not verify")
	}
}
