package client

import "fmt"

// Status 打印当前注册状态与网络配置信息。
func Status() error {
	cfg, err := LoadClientConfig()
	if err != nil {
		return fmt.Errorf("not registered; run 'mesh join' first")
	}
	fmt.Printf("Server:  %s\nIP:      %s\nNetwork: %s\nDevice:  %s\n",
		cfg.ServerDomain, cfg.DeviceIP, cfg.NetworkCIDR, cfg.DeviceID)
	return nil
}
