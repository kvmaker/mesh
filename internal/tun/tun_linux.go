//go:build linux

package tun

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"

	"golang.org/x/sys/unix"
	wgtun "golang.zx2c4.com/wireguard/tun"
)

// createTUNNative opens /dev/net/tun WITHOUT IFF_VNET_HDR, then wraps the fd
// with the wireguard/tun library's CreateTUNFromFile.
//
// 不带 IFF_VNET_HDR 是关键：库的 initFromFlags 检测到无 VNET_HDR 时设
// vnetHdr=false，kernel 不做 TSO/GSO/checksum offload，TCP 包由 kernel 软件
// 算好 checksum，mesh 转发的包天然正确（修复 B00 TCP checksum 损坏）。
// 代价：batchSize=1（无批量读），但 mesh 本就单包处理，无性能影响。
func createTUNNative(name string, mtu int) (Device, string, error) {
	fd, err := unix.Open("/dev/net/tun", unix.O_RDWR|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, "", fmt.Errorf("open /dev/net/tun: %w", err)
	}

	ifr, err := unix.NewIfreq(name)
	if err != nil {
		unix.Close(fd)
		return nil, "", fmt.Errorf("new ifreq %s: %w", name, err)
	}
	ifr.SetUint16(unix.IFF_TUN | unix.IFF_NO_PI) // 注意：无 IFF_VNET_HDR
	if err := unix.IoctlIfreq(fd, unix.TUNSETIFF, ifr); err != nil {
		unix.Close(fd)
		return nil, "", fmt.Errorf("TUNSETIFF: %w", err)
	}

	if err := unix.SetNonblock(fd, true); err != nil {
		unix.Close(fd)
		return nil, "", fmt.Errorf("set nonblock: %w", err)
	}

	file := os.NewFile(uintptr(fd), "/dev/net/tun")
	dev, err := wgtun.CreateTUNFromFile(file, mtu)
	if err != nil {
		file.Close() // CreateTUNFromFile 失败时确保 fd 不泄漏
		return nil, "", fmt.Errorf("create TUN %s: %w", name, err)
	}
	actualName, err := dev.Name()
	if err != nil {
		dev.Close()
		return nil, "", fmt.Errorf("get TUN name: %w", err)
	}
	return dev, actualName, nil
}

func ConfigureInterface(ifaceName, localIP, network string) error {
	_, ipNet, err := net.ParseCIDR(network)
	if err != nil {
		return fmt.Errorf("parse network: %w", err)
	}
	ones, _ := ipNet.Mask.Size()
	addr := fmt.Sprintf("%s/%d", localIP, ones)

	if out, err := exec.Command("ip", "addr", "add", addr, "dev", ifaceName).CombinedOutput(); err != nil {
		if !strings.Contains(string(out), "exists") {
			return fmt.Errorf("ip addr add: %s: %w", out, err)
		}
	}
	if out, err := exec.Command("ip", "link", "set", ifaceName, "up").CombinedOutput(); err != nil {
		return fmt.Errorf("ip link set up: %s: %w", out, err)
	}
	if out, err := exec.Command("ip", "route", "add", network, "dev", ifaceName).CombinedOutput(); err != nil {
		if !strings.Contains(string(out), "exists") {
			return fmt.Errorf("ip route add: %s: %w", out, err)
		}
	}
	return nil
}
