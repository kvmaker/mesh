package client

import (
	"fmt"
	"net/http"
	"os"
)

// Leave 从 mesh 网络注销当前设备，清理本地 WireGuard 配置和配置文件
func Leave() error {
	cfg, err := LoadClientConfig()
	if err != nil {
		return fmt.Errorf("not registered; run 'mesh join' first")
	}

	// 向服务器发送注销请求（尽力而为，失败不阻止本地清理）
	if cfg.DeviceID != "" {
		url := fmt.Sprintf("http://%s/api/devices/%s", cfg.ServerAddr, cfg.DeviceID)
		req, err := http.NewRequest(http.MethodDelete, url, nil)
		if err == nil {
			req.Header.Set("Authorization", "Bearer "+cfg.DeviceSecret)
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				fmt.Printf("Warning: could not notify server: %v\n", err)
			} else {
				resp.Body.Close()
			}
		}
	}

	// 关闭本地 WireGuard 接口（尽力而为）
	if err := TeardownWireGuard("wg0"); err != nil {
		fmt.Printf("Warning: could not teardown WireGuard: %v\n", err)
	}

	// 删除本地配置目录
	if err := os.RemoveAll(ConfigDir()); err != nil {
		return fmt.Errorf("remove config dir: %w", err)
	}

	fmt.Println("Left the mesh network. Local config removed.")
	return nil
}
