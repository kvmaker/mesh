package client

import (
	"fmt"
	"os"
)

// Leave 删除本地配置目录，撤销设备注册（本地侧）。
func Leave() error {
	if _, err := LoadClientConfig(); err != nil {
		return fmt.Errorf("not registered")
	}
	if err := os.RemoveAll(ConfigDir()); err != nil {
		return fmt.Errorf("remove config: %w", err)
	}
	fmt.Println("Left mesh network. Config removed.")
	return nil
}
