package wg

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"

	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

type SetupConfig struct {
	Interface  string // e.g. "wg0"
	ListenPort int
	PrivateKey string
	Address    string // e.g. "10.100.0.1/24"
}

type PeerStatus struct {
	PublicKey      string
	AllowedIPs     string
	LastHandshake  string
	TransferRx     string
	TransferTx     string
}

func GenerateKeyPair() (privateKey, publicKey string, err error) {
	key, err := wgtypes.GeneratePrivateKey()
	if err != nil {
		return "", "", fmt.Errorf("generate private key: %w", err)
	}
	priv := key.String()
	pub := key.PublicKey().String()
	return priv, pub, nil
}

func Setup(cfg SetupConfig) error {
	// 创建接口
	if err := run("ip", "link", "add", cfg.Interface, "type", "wireguard"); err != nil {
		// 接口可能已存在
		if !strings.Contains(err.Error(), "exists") {
			return fmt.Errorf("create interface: %w", err)
		}
	}

	// 写入私钥并配置
	privKeyCmd := exec.Command("wg", "set", cfg.Interface,
		"listen-port", fmt.Sprintf("%d", cfg.ListenPort),
		"private-key", "/dev/stdin")
	privKeyCmd.Stdin = strings.NewReader(cfg.PrivateKey)
	if out, err := privKeyCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("wg set: %s: %w", out, err)
	}

	// 配置 IP 地址
	if err := run("ip", "address", "add", cfg.Address, "dev", cfg.Interface); err != nil {
		if !strings.Contains(err.Error(), "exists") {
			return fmt.Errorf("add address: %w", err)
		}
	}

	// 启动接口
	if err := run("ip", "link", "set", cfg.Interface, "up"); err != nil {
		return fmt.Errorf("bring up interface: %w", err)
	}

	return nil
}

func AddPeer(iface, publicKey, allowedIP string) error {
	return run("wg", "set", iface, "peer", publicKey, "allowed-ips", allowedIP+"/32", "persistent-keepalive", "25")
}

func RemovePeer(iface, publicKey string) error {
	return run("wg", "set", iface, "peer", publicKey, "remove")
}

func Show(iface string) ([]PeerStatus, error) {
	out, err := exec.Command("wg", "show", iface, "dump").Output()
	if err != nil {
		return nil, fmt.Errorf("wg show: %w", err)
	}

	var peers []PeerStatus
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	// 跳过第一行（接口自身信息）
	for _, line := range lines[1:] {
		fields := strings.Split(line, "\t")
		if len(fields) < 5 {
			continue
		}
		peers = append(peers, PeerStatus{
			PublicKey:     fields[0],
			AllowedIPs:    fields[3],
			LastHandshake: fields[4],
			TransferRx:    fields[5],
			TransferTx:    fields[6],
		})
	}
	return peers, nil
}

func Teardown(iface string) error {
	return run("ip", "link", "del", iface)
}

func run(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s %s: %s: %w", name, strings.Join(args, " "), stderr.String(), err)
	}
	return nil
}
