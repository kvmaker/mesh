package client

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// acquireInstanceLock 抢占 ConfigDir 下 mesh.lock 的排他锁。
//
// 成功时返回非 nil 的 *os.File，调用方负责 defer Close；fd 关闭时内核自动
// 释放 flock，无需清理 stale lock。返回 syscall.EWOULDBLOCK 时表示另一
// 个 mesh up 已在运行，调用方应直接返回错误退出。
func acquireInstanceLock() (*os.File, error) {
	return acquireInstanceLockAt(filepath.Join(ConfigDir(), "mesh.lock"))
}

// acquireInstanceLockAt 是 acquireInstanceLock 的可注入路径版本，方便单测
// 使用 t.TempDir() 而不需要污染 ~/.mesh。
func acquireInstanceLockAt(path string) (*os.File, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, fmt.Errorf("mkdir lock dir: %w", err)
	}
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0600)
	if err != nil {
		return nil, fmt.Errorf("open lock file: %w", err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, fmt.Errorf("another mesh instance is already running (lock: %s); use 'mesh status' to check", path)
		}
		return nil, fmt.Errorf("acquire lock: %w", err)
	}
	return f, nil
}
