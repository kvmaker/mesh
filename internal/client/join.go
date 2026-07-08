package client

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"
)

// Join 向服务器注册当前设备，并将返回的配置保存到本地。
// 该操作不需要 root 权限。
//
// insecureTLS 为真时跳过 TLS 证书校验，仅在 e2e 测试（server 使用自签证书）时使用。
func Join(domain, tok string, insecureTLS bool) error {
	if err := os.MkdirAll(ConfigDir(), 0700); err != nil {
		return err
	}
	if _, err := LoadClientConfig(); err == nil {
		return fmt.Errorf("already registered; run 'mesh leave' first")
	}

	hostname, _ := os.Hostname()
	reqBody, _ := json.Marshal(map[string]string{
		"token":    tok,
		"hostname": hostname,
	})
	url := fmt.Sprintf("https://%s/api/devices/register", domain)

	httpClient := &http.Client{Timeout: 10 * time.Second}
	if insecureTLS {
		httpClient.Transport = &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		}
	}

	resp, err := httpClient.Post(url, "application/json", bytes.NewReader(reqBody))
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusUnauthorized:
		return fmt.Errorf("invalid token")
	case http.StatusOK:
		// ok
	default:
		return fmt.Errorf("registration failed: HTTP %d", resp.StatusCode)
	}

	var regResp struct {
		AssignedIP   string `json:"assigned_ip"`
		DeviceSecret string `json:"device_secret"`
		DeviceID     string `json:"device_id"`
		NetworkCIDR  string `json:"network_cidr"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&regResp); err != nil {
		return fmt.Errorf("parse response: %w", err)
	}

	cfg := &ClientConfig{
		ServerDomain: domain,
		DeviceSecret: regResp.DeviceSecret,
		DeviceIP:     regResp.AssignedIP,
		DeviceID:     regResp.DeviceID,
		NetworkCIDR:  regResp.NetworkCIDR,
		InsecureTLS:  insecureTLS,
	}
	if err := SaveClientConfig(cfg); err != nil {
		return err
	}

	fmt.Printf("Registered!\n  IP: %s\n  Network: %s\n", regResp.AssignedIP, regResp.NetworkCIDR)
	return nil
}
