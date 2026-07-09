package client

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// acquireInstanceLock 抢占 /var/run/mesh.lock 排他锁。
//
// 用 /var/run 而不是 ~/.mesh：launchd / sudo / sudo -i / sudo -u 等
// 不同调用方式下进程的 $HOME 会变化（root 默认 /var/root，普通用户
// /Users/<name>，sudo -i 还会覆盖 HOME），依赖 $HOME 会让锁路径分裂，
// 多份 mesh up 实例共存。/var/run 是 root 可写的固定路径，对所有调
// 用方式统一。
//
// mesh up 本身需要 root 权限创建 TUN 设备，所以路径使用 root 才能写入
// 的位置是合理的。
//
// 成功时返回非 nil 的 *os.File，调用方负责 defer Close；fd 关闭时内核
// 自动释放 flock，无需清理 stale lock。返回 syscall.EWOULDBLOCK 时表示
// 另一个 mesh up 已在运行，调用方应直接返回错误退出。
func acquireInstanceLock() (*os.File, error) {
	return acquireInstanceLockAt("/var/run/mesh.lock")
}

// acquireInstanceLockAt 是 acquireInstanceLock 的可注入路径版本，方便单测
// 使用 t.TempDir() 而不需要写入 /var/run。
func acquireInstanceLockAt(path string) (*os.File, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, fmt.Errorf("mkdir lock dir: %w", err)
	}
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0600)
	if err != nil {
		// /var/run 仅 root 可写；普通用户在此处最先撞到权限错误（早于 TUN
		// 创建），给出明确的 sudo 提示，避免用户困惑于 "permission denied"。
		if os.IsPermission(err) {
			return nil, fmt.Errorf("mesh up requires root; run with sudo (lock: %s)", path)
		}
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
