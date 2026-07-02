package token

import (
	"database/sql"
	"testing"

	"github.com/maxyu/mesh/internal/db"
)

func setupTestDB(t *testing.T) *sql.DB {
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

func TestGenerate(t *testing.T) {
	tok, err := Generate()
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}
	if len(tok) != 64 { // 32 bytes hex encoded = 64 chars
		t.Fatalf("expected 64 char token, got %d", len(tok))
	}
}

func TestSaveAndLoad(t *testing.T) {
	database := setupTestDB(t)

	tok, _ := Generate()
	if err := Save(database, tok); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	loaded, err := Load(database)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if loaded != tok {
		t.Fatalf("expected %q, got %q", tok, loaded)
	}
}

func TestVerify(t *testing.T) {
	database := setupTestDB(t)

	tok, _ := Generate()
	Save(database, tok)

	if !Verify(database, tok) {
		t.Fatal("expected valid token to verify")
	}
	if Verify(database, "wrong-token") {
		t.Fatal("expected invalid token to fail")
	}
}

func TestSaveOverwrites(t *testing.T) {
	database := setupTestDB(t)

	tok1, _ := Generate()
	Save(database, tok1)

	tok2, _ := Generate()
	Save(database, tok2)

	loaded, _ := Load(database)
	if loaded != tok2 {
		t.Fatalf("expected new token %q, got %q", tok2, loaded)
	}
	if Verify(database, tok1) {
		t.Fatal("old token should no longer verify")
	}
}
