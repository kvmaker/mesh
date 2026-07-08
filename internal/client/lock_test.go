package client

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestAcquireInstanceLockAt 验证单实例锁的三个关键语义：
//  1. 首次抢占成功
//  2. 同一 fd 已持锁时再次抢占返回 EWOULDBLOCK 风格的错误
//  3. 锁 fd 关闭后再次抢占成功（无 stale lock 问题）
func TestAcquireInstanceLockAt(t *testing.T) {
	dir := t.TempDir()
	lockPath := filepath.Join(dir, "mesh.lock")

	// 1. 首次抢占
	first, err := acquireInstanceLockAt(lockPath)
	if err != nil {
		t.Fatalf("first acquire failed: %v", err)
	}
	if first == nil {
		t.Fatal("first acquire returned nil fd")
	}
	t.Cleanup(func() { first.Close() })

	// 2. 锁已被持时再抢，应当失败且错误信息含 "already running"
	second, err := acquireInstanceLockAt(lockPath)
	if err == nil {
		second.Close()
		t.Fatal("second acquire succeeded while first held the lock; want EWOULDBLOCK")
	}
	if second != nil {
		t.Errorf("second acquire returned non-nil fd on error: %v", second)
	}
	if !strings.Contains(err.Error(), "already running") {
		t.Errorf("error message should mention 'already running', got: %v", err)
	}
	if !errors.Is(err, os.ErrExist) && !strings.Contains(err.Error(), "file already") && !strings.Contains(err.Error(), "EWOULDBLOCK") {
		// 不强制要求 errors.Is，仅日志记录
		t.Logf("note: error type is %T (not os.ErrExist), may be platform-specific", err)
	}

	// 3. 释放后再次抢占应成功（无 stale lock）
	if err := first.Close(); err != nil {
		t.Fatalf("first.Close failed: %v", err)
	}
	third, err := acquireInstanceLockAt(lockPath)
	if err != nil {
		t.Fatalf("third acquire after release failed: %v", err)
	}
	t.Cleanup(func() { third.Close() })
}
