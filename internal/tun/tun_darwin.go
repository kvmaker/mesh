//go:build darwin

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
	mask := net.CIDRMask(ones, 32)
	maskStr := fmt.Sprintf("%d.%d.%d.%d", mask[0], mask[1], mask[2], mask[3])

	if out, err := exec.Command("ifconfig", ifaceName, "inet", localIP, localIP, "netmask", maskStr, "up").CombinedOutput(); err != nil {
		return fmt.Errorf("ifconfig: %s: %w", out, err)
	}
	if out, err := exec.Command("route", "-n", "add", "-net", network, "-interface", ifaceName).CombinedOutput(); err != nil {
		return fmt.Errorf("route add: %s: %w", out, err)
	}
	return nil
}
