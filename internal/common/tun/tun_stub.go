//go:build !linux && !darwin

package tun

import (
	"fmt"
	"runtime"
)

// ConfigureInterface 在 linux/darwin 之外的平台没有真正实现。
//
// TUN 设备的地址/路由配置高度依赖平台特定命令（linux 用 ip、darwin 用
// ifconfig+route），其它平台（如 windows）需要各自的实现。提供此桩是为了
// 让代码在这些平台上仍能编译（例如交叉编译做静态检查），运行时会明确报错
// 而不是静默失败。
func ConfigureInterface(ifaceName, localIP, network string) error {
	return fmt.Errorf("ConfigureInterface not implemented on %s", runtime.GOOS)
}
