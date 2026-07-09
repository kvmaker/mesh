package db

import (
	"os"
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

// TestMigrateCreatesMetaTable 补充断言 meta 表也被创建。
func TestMigrateCreatesMetaTable(t *testing.T) {
	d, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if err := Migrate(d); err != nil {
		t.Fatal(err)
	}
	var name string
	d.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name='meta'").Scan(&name)
	if name != "meta" {
		t.Fatalf("expected meta table, got %q", name)
	}
}

// TestMigrateIdempotent 验证 Migrate 可重复执行（IF NOT EXISTS），二次调用不报错。
func TestMigrateIdempotent(t *testing.T) {
	d, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if err := Migrate(d); err != nil {
		t.Fatalf("first migrate: %v", err)
	}
	if err := Migrate(d); err != nil {
		t.Fatalf("second migrate should be idempotent: %v", err)
	}
}

// TestOpenMkdirError 覆盖 Open 的 MkdirAll 错误分支：当父路径中间段是一个已存在
// 的普通文件时，MkdirAll 无法在其下创建目录，返回错误。
func TestOpenMkdirError(t *testing.T) {
	dir := t.TempDir()
	// 先创建一个普通文件 "afile"，再要求把它当成目录来放 db，触发 MkdirAll 失败。
	blocker := filepath.Join(dir, "afile")
	if err := os.WriteFile(blocker, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := Open(filepath.Join(blocker, "sub", "test.db"))
	if err == nil {
		t.Fatal("expected error when db dir cannot be created")
	}
}
