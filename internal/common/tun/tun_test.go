package tun

import (
	"runtime"
	"testing"
)

// TestOffset 验证平台相关的 TUN 读写偏移量：darwin 为 4（utun AF 前缀），
// 其余平台为 0（linux B00 方案 D 不带 virtio net header）。
func TestOffset(t *testing.T) {
	got := Offset()
	want := 0
	if runtime.GOOS == "darwin" {
		want = 4
	}
	if got != want {
		t.Fatalf("Offset() on %s = %d, want %d", runtime.GOOS, got, want)
	}
}

// TestDefaultTUNName 验证默认 TUN 设备名：darwin 用 "utun"，其余用 "mesh0"。
func TestDefaultTUNName(t *testing.T) {
	got := DefaultTUNName()
	want := "mesh0"
	if runtime.GOOS == "darwin" {
		want = "utun"
	}
	if got != want {
		t.Fatalf("DefaultTUNName() on %s = %q, want %q", runtime.GOOS, got, want)
	}
}
