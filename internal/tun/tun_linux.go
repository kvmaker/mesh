//go:build linux

package tun

import (
	"fmt"
	"net"
	"os/exec"
)

func ConfigureInterface(ifaceName, localIP, network string) error {
	_, ipNet, err := net.ParseCIDR(network)
	if err != nil {
		return fmt.Errorf("parse network: %w", err)
	}
	ones, _ := ipNet.Mask.Size()
	addr := fmt.Sprintf("%s/%d", localIP, ones)

	if out, err := exec.Command("ip", "addr", "add", addr, "dev", ifaceName).CombinedOutput(); err != nil {
		return fmt.Errorf("ip addr add: %s: %w", out, err)
	}
	if out, err := exec.Command("ip", "link", "set", ifaceName, "up").CombinedOutput(); err != nil {
		return fmt.Errorf("ip link set up: %s: %w", out, err)
	}
	if out, err := exec.Command("ip", "route", "add", network, "dev", ifaceName).CombinedOutput(); err != nil {
		return fmt.Errorf("ip route add: %s: %w", out, err)
	}
	return nil
}
