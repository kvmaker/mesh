package tun

import (
	"fmt"
	"runtime"

	wgtun "golang.zx2c4.com/wireguard/tun"
)

type Device = wgtun.Device

// Offset 返回平台所需的 TUN 读写偏移量。
//
// darwin: utun 每帧前缀 4 字节 AF family packet info，offset = 4。
// linux: golang.zx2c4.com/wireguard/tun 以 IFF_TUN|IFF_NO_PI|IFF_VNET_HDR 打开
//
//	设备，每帧前缀 10 字节 virtio net header（virtioNetHdrLen）。Write 路径的
//	handleGRO 会硬性校验 offset >= virtioNetHdrLen，传 0 会返回 "invalid offset"
//	导致所有从网络收到的包都写不进 TUN。因此 Linux 必须 offset = 10。
func Offset() int {
	switch runtime.GOOS {
	case "darwin":
		return 4
	case "linux":
		return 10
	default:
		return 0
	}
}

func CreateTUN(name string, mtu int) (Device, string, error) {
	dev, err := wgtun.CreateTUN(name, mtu)
	if err != nil {
		return nil, "", fmt.Errorf("create TUN %s: %w", name, err)
	}
	actualName, err := dev.Name()
	if err != nil {
		dev.Close()
		return nil, "", fmt.Errorf("get TUN name: %w", err)
	}
	return dev, actualName, nil
}

func DefaultTUNName() string {
	if runtime.GOOS == "darwin" {
		return "utun"
	}
	return "mesh0"
}
