package token

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/maxyu/mesh/internal/server/db"
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

// TestGenerateUnique 验证每次 Generate 产出的 token 不重复（随机性 sanity check）。
func TestGenerateUnique(t *testing.T) {
	seen := make(map[string]bool)
	for i := 0; i < 100; i++ {
		tok, err := Generate()
		if err != nil {
			t.Fatal(err)
		}
		if seen[tok] {
			t.Fatalf("duplicate token generated: %s", tok)
		}
		seen[tok] = true
	}
}

// TestSaveReplacesExisting 验证 Save 使用 INSERT OR REPLACE：二次保存覆盖旧值，
// 旧 token 不再通过校验。
func TestSaveReplacesExisting(t *testing.T) {
	d := setup(t)
	if err := Save(d, "first"); err != nil {
		t.Fatalf("first save: %v", err)
	}
	if err := Save(d, "second"); err != nil {
		t.Fatalf("second save: %v", err)
	}
	got, err := Load(d)
	if err != nil {
		t.Fatal(err)
	}
	if got != "second" {
		t.Fatalf("expected 'second' after replace, got %q", got)
	}
	if Verify(d, "first") {
		t.Fatal("old token should no longer verify after replace")
	}
}

// TestLoadEmpty 验证空数据库（无 token 行）时 Load 返回错误。
func TestLoadEmpty(t *testing.T) {
	d := setup(t)
	_, err := Load(d)
	if err == nil {
		t.Fatal("expected error loading token from empty db")
	}
}

// TestVerifyEmptyDB 验证空库时 Verify 因 Load 失败返回 false（覆盖 Load err 分支）。
func TestVerifyEmptyDB(t *testing.T) {
	d := setup(t)
	if Verify(d, "anything") {
		t.Fatal("verify against empty db should be false")
	}
}
