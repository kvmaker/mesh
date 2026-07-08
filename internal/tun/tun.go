package tun

import (
	"runtime"

	wgtun "golang.zx2c4.com/wireguard/tun"
)

type Device = wgtun.Device

// Offset 返回平台所需的 TUN 读写偏移量。
//
// darwin: utun 每帧前缀 4 字节 AF family packet info，offset = 4。
// linux: B00 方案 D 后 mesh 自开 /dev/net/tun 不带 IFF_VNET_HDR（见
//
//	createTUNNative），wireguard/tun 库检测到无 VNET_HDR 时 vnetHdr=false，
//	无 virtio net header，offset = 0（撤销 T04 的 offset=10 workaround）。
func Offset() int {
	switch runtime.GOOS {
	case "darwin":
		return 4
	default: // linux 及其它：无 virtio header（B00 方案 D 不带 IFF_VNET_HDR）
		return 0
	}
}

// CreateTUN 创建 TUN 设备，平台分发由 createTUNNative 的 build tag 处理。
func CreateTUN(name string, mtu int) (Device, string, error) {
	return createTUNNative(name, mtu)
}

func DefaultTUNName() string {
	if runtime.GOOS == "darwin" {
		return "utun"
	}
	return "mesh0"
}
