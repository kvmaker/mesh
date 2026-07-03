package client

import (
	"context"
	"fmt"

	"github.com/maxyu/mesh/internal/tunnel"
)

// Up 读取本地配置，创建 TUN 设备并建立 WebSocket 隧道。
// 该操作需要 root 权限（TUN 设备创建）。
func Up(ctx context.Context) error {
	cfg, err := LoadClientConfig()
	if err != nil {
		return fmt.Errorf("not registered; run 'mesh join' first")
	}

	wsURL := fmt.Sprintf("wss://%s/tunnel", cfg.ServerDomain)
	tc, err := tunnel.NewTunnelClient(wsURL, cfg.DeviceSecret, cfg.DeviceIP, cfg.NetworkCIDR, 1300)
	if err != nil {
		return fmt.Errorf("setup tunnel: %w", err)
	}
	defer tc.Close()

	fmt.Printf("Mesh VPN up (IP: %s)\n", cfg.DeviceIP)
	return tc.Run(ctx)
}
