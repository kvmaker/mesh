package client

import (
	"fmt"

	"github.com/maxyu/mesh/internal/wg"
)

// Status 显示当前设备的连接状态
func Status() error {
	cfg, err := LoadClientConfig()
	if err != nil {
		return fmt.Errorf("not registered; run 'mesh join' first")
	}

	fmt.Printf("Server:    %s\n", cfg.ServerAddr)
	fmt.Printf("Device IP: %s\n", cfg.DeviceIP)

	// 查询 WireGuard 接口状态
	peers, err := wg.Show("wg0")
	if err != nil {
		fmt.Printf("WireGuard: not running\n")
		return nil
	}

	if len(peers) == 0 {
		fmt.Printf("WireGuard: running (no peers)\n")
		return nil
	}

	fmt.Printf("WireGuard: connected\n")
	for _, p := range peers {
		pubShort := p.PublicKey
		if len(pubShort) > 12 {
			pubShort = pubShort[:12] + "..."
		}
		fmt.Printf("  Peer:           %s\n", pubShort)
		fmt.Printf("  Last handshake: %s\n", p.LastHandshake)
		fmt.Printf("  Transfer:       ↓%s ↑%s\n", p.TransferRx, p.TransferTx)
	}

	return nil
}
