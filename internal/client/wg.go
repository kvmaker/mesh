package client

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// WGConfig 是客户端 WireGuard 配置
type WGConfig struct {
	Interface  string
	PrivateKey string
	Address    string
	PeerPubKey string
	Endpoint   string
	AllowedIPs string
}

// SetupWireGuard 根据当前操作系统配置 WireGuard 接口
func SetupWireGuard(cfg WGConfig) error {
	switch runtime.GOOS {
	case "linux":
		return setupLinux(cfg)
	case "darwin":
		return setupDarwin(cfg)
	default:
		return fmt.Errorf("unsupported OS: %s", runtime.GOOS)
	}
}

// TeardownWireGuard 关闭并清理 WireGuard 接口
func TeardownWireGuard(iface string) error {
	switch runtime.GOOS {
	case "linux":
		return runCmd("ip", "link", "del", iface)
	case "darwin":
		// macOS 使用 wg-quick down
		_ = runCmd("wg-quick", "down", iface)
		_ = runCmd("rm", "-f", fmt.Sprintf("/var/run/wireguard/%s.sock", iface))
		return nil
	default:
		return nil
	}
}

func setupLinux(cfg WGConfig) error {
	// 创建 WireGuard 接口（允许已存在）
	_ = runCmd("ip", "link", "add", cfg.Interface, "type", "wireguard")

	// 配置私钥、peer、endpoint 和 allowed-ips
	cmd := exec.Command("wg", "set", cfg.Interface,
		"private-key", "/dev/stdin",
		"peer", cfg.PeerPubKey,
		"endpoint", cfg.Endpoint,
		"allowed-ips", cfg.AllowedIPs,
		"persistent-keepalive", "25")
	cmd.Stdin = strings.NewReader(cfg.PrivateKey)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("wg set: %s: %w", string(out), err)
	}

	// 添加 IP 地址（允许已存在）
	if err := runCmd("ip", "address", "add", cfg.Address+"/24", "dev", cfg.Interface); err != nil {
		if !strings.Contains(err.Error(), "exists") {
			return fmt.Errorf("ip address add: %w", err)
		}
	}

	// 启动接口
	return runCmd("ip", "link", "set", cfg.Interface, "up")
}

func setupDarwin(cfg WGConfig) error {
	// macOS 使用 wg-quick 配置
	confContent := fmt.Sprintf(`[Interface]
PrivateKey = %s
Address = %s/24

[Peer]
PublicKey = %s
Endpoint = %s
AllowedIPs = %s
PersistentKeepalive = 25
`, cfg.PrivateKey, cfg.Address, cfg.PeerPubKey, cfg.Endpoint, cfg.AllowedIPs)

	confPath := fmt.Sprintf("/tmp/%s.conf", cfg.Interface)
	if err := os.WriteFile(confPath, []byte(confContent), 0600); err != nil {
		return fmt.Errorf("write wg config: %w", err)
	}
	defer os.Remove(confPath)

	return runCmd("wg-quick", "up", confPath)
}

func runCmd(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%s %s: %s: %w", name, strings.Join(args, " "), string(out), err)
	}
	return nil
}
