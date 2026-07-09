package client

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/dustin/go-humanize"
)

// Status 打印当前注册状态与隧道运行时统计信息。
func Status() error {
	cfg, err := LoadClientConfig()
	if err != nil {
		return fmt.Errorf("not registered; run 'mesh join' first")
	}
	fmt.Printf("Server:  %s\n", cfg.ServerDomain)
	fmt.Printf("IP:      %s\n", cfg.DeviceIP)
	fmt.Printf("Network: %s\n", cfg.NetworkCIDR)
	fmt.Printf("Device:  %s\n", cfg.DeviceID)

	stats, err := loadTunnelStats()
	if err != nil {
		fmt.Printf("\nTunnel:  not running\n")
		return nil
	}

	if !isProcessAlive(stats.PID) || time.Since(stats.UpdatedAt) > 10*time.Second {
		fmt.Printf("\nTunnel:  not running (stale)\n")
		return nil
	}

	fmt.Println()
	if !stats.Connected {
		fmt.Printf("Tunnel:  reconnecting...\n")
		return nil
	}

	fmt.Printf("Tunnel:  connected\n")
	fmt.Printf("Uptime:  %s\n", formatDuration(time.Since(stats.ConnectedAt)))
	if stats.LastRTT > 0 {
		if stats.LastRTT < 1000 {
			fmt.Printf("RTT:     %dμs\n", stats.LastRTT)
		} else {
			fmt.Printf("RTT:     %.1fms\n", float64(stats.LastRTT)/1000)
		}
	}
	fmt.Printf("TX:      %s pkts / %s\n", humanize.Comma(int64(stats.TxPackets)), humanize.IBytes(stats.TxBytes))
	fmt.Printf("RX:      %s pkts / %s\n", humanize.Comma(int64(stats.RxPackets)), humanize.IBytes(stats.RxBytes))
	if !stats.LastActive.IsZero() {
		fmt.Printf("Last:    %s ago\n", formatDuration(time.Since(stats.LastActive)))
	}
	return nil
}

func loadTunnelStats() (*ClientStats, error) {
	path := filepath.Join(ConfigDir(), "status.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var stats ClientStats
	if err := json.Unmarshal(data, &stats); err != nil {
		return nil, err
	}
	return &stats, nil
}

func isProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return proc.Signal(syscall.Signal(0)) == nil
}

func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm%ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	return fmt.Sprintf("%dh%dm", h, m)
}
