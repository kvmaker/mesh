package client

import (
	"context"
	"crypto/tls"
	"fmt"
)

// Up 读取本地配置，创建 TUN 设备并建立 WebSocket 隧道。
// 该操作需要 root 权限（TUN 设备创建）。
func Up(ctx context.Context) error {
	// 单实例保护：抢占排他锁，已有 mesh up 在跑时立即返回错误退出，
	// 避免多份实例同时持有 TUN 设备和 WebSocket 连接。
	lock, err := acquireInstanceLock()
	if err != nil {
		return err
	}
	defer lock.Close()

	cfg, err := LoadClientConfig()
	if err != nil {
		return fmt.Errorf("not registered; run 'mesh join' first")
	}

	wsURL := fmt.Sprintf("wss://%s/tunnel", cfg.ServerDomain)

	var tlsCfg *tls.Config
	if cfg.InsecureTLS {
		tlsCfg = &tls.Config{InsecureSkipVerify: true}
	}

	tc, err := NewTunnelClient(wsURL, cfg.DeviceSecret, cfg.DeviceIP, cfg.NetworkCIDR, 1300, ConfigDir(), tlsCfg)
	if err != nil {
		return fmt.Errorf("setup tunnel: %w", err)
	}
	defer tc.Close()

	fmt.Printf("Mesh VPN up (IP: %s)\n", cfg.DeviceIP)
	return tc.Run(ctx)
}
