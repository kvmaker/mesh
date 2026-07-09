//go:build !linux

package tun

import (
	"fmt"

	wgtun "golang.zx2c4.com/wireguard/tun"
)

// createTUNNative 非 linux 平台（如 darwin）直接用 wireguard/tun 库的
// CreateTUN。这些平台无 IFF_VNET_HDR/offload 问题，库的默认行为正确。
func createTUNNative(name string, mtu int) (Device, string, error) {
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
