package tun

import (
	"fmt"
	"runtime"

	wgtun "golang.zx2c4.com/wireguard/tun"
)

type Device = wgtun.Device

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
