package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"

	"github.com/maxyu/mesh/internal/wg"
)

// ClientConfig 是客户端本地配置，存储在 ~/.mesh/config.json
type ClientConfig struct {
	ServerAddr   string `json:"server_addr"`
	DeviceSecret string `json:"device_secret"`
	DeviceIP     string `json:"device_ip"`
	DeviceID     string `json:"device_id"`
}

// ConfigDir 返回客户端配置目录路径（~/.mesh）
func ConfigDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".mesh")
}

// LoadClientConfig 从 ~/.mesh/config.json 加载客户端配置
func LoadClientConfig() (*ClientConfig, error) {
	data, err := os.ReadFile(filepath.Join(ConfigDir(), "config.json"))
	if err != nil {
		return nil, err
	}
	var cfg ClientConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	return &cfg, nil
}

// SaveClientConfig 将客户端配置保存到 ~/.mesh/config.json（权限 0600）
func SaveClientConfig(cfg *ClientConfig) error {
	cfgData, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	cfgPath := filepath.Join(ConfigDir(), "config.json")
	return os.WriteFile(cfgPath, cfgData, 0600)
}

// Join 向服务器注册当前设备并配置本地 WireGuard 接口
func Join(serverAddr, token string) error {
	configDir := ConfigDir()
	if err := os.MkdirAll(configDir, 0700); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}

	// 检查是否已注册
	if _, err := LoadClientConfig(); err == nil {
		return fmt.Errorf("already registered; run 'mesh leave' first")
	}

	// 生成 WireGuard 密钥对
	privKey, pubKey, err := wg.GenerateKeyPair()
	if err != nil {
		return fmt.Errorf("generate keys: %w", err)
	}

	// 保存私钥（权限 0600）
	keyPath := filepath.Join(configDir, "private.key")
	if err := os.WriteFile(keyPath, []byte(privKey), 0600); err != nil {
		return fmt.Errorf("save private key: %w", err)
	}

	// 获取主机名
	hostname, _ := os.Hostname()

	// 构造注册请求
	reqBody, _ := json.Marshal(map[string]string{
		"token":      token,
		"public_key": pubKey,
		"hostname":   hostname,
	})

	url := fmt.Sprintf("http://%s/api/devices/register", serverAddr)
	resp, err := http.Post(url, "application/json", bytes.NewReader(reqBody))
	if err != nil {
		return fmt.Errorf("connect to server: %w", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusUnauthorized:
		return fmt.Errorf("invalid token")
	case http.StatusOK:
		// 正常继续
	default:
		return fmt.Errorf("registration failed: HTTP %d", resp.StatusCode)
	}

	var regResp struct {
		AssignedIP      string `json:"assigned_ip"`
		ServerPublicKey string `json:"server_public_key"`
		ServerEndpoint  string `json:"server_endpoint"`
		NetworkCIDR     string `json:"network_cidr"`
		DeviceSecret    string `json:"device_secret"`
		DeviceID        string `json:"device_id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&regResp); err != nil {
		return fmt.Errorf("parse response: %w", err)
	}

	// 保存客户端配置
	clientCfg := &ClientConfig{
		ServerAddr:   serverAddr,
		DeviceSecret: regResp.DeviceSecret,
		DeviceIP:     regResp.AssignedIP,
		DeviceID:     regResp.DeviceID,
	}
	if err := SaveClientConfig(clientCfg); err != nil {
		return fmt.Errorf("save config: %w", err)
	}

	// 配置本地 WireGuard 接口
	if err := SetupWireGuard(WGConfig{
		Interface:  "wg0",
		PrivateKey: privKey,
		Address:    regResp.AssignedIP,
		PeerPubKey: regResp.ServerPublicKey,
		Endpoint:   regResp.ServerEndpoint,
		AllowedIPs: regResp.NetworkCIDR,
	}); err != nil {
		return fmt.Errorf("setup wireguard: %w", err)
	}

	// 启动后台心跳
	StartHeartbeat(serverAddr, regResp.DeviceSecret)

	fmt.Printf("Joined successfully!\n")
	fmt.Printf("  IP:      %s\n", regResp.AssignedIP)
	fmt.Printf("  Network: %s\n", regResp.NetworkCIDR)
	fmt.Printf("  Server:  %s\n", serverAddr)
	return nil
}
