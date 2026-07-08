package client

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"text/tabwriter"
	"os"
	"time"
)

type peerInfo struct {
	Name   string `json:"name"`
	IP     string `json:"ip"`
	Online bool   `json:"online"`
}

// Peers 从服务端获取并展示当前网络中的所有设备。
func Peers() error {
	cfg, err := LoadClientConfig()
	if err != nil {
		return fmt.Errorf("not registered; run 'mesh join' first")
	}

	// 仅在 e2e 测试模式（join 时带 --insecure）下跳过证书校验，
	// 生产环境必须正常校验 Let's Encrypt 证书。
	tlsCfg := &tls.Config{}
	if cfg.InsecureTLS {
		tlsCfg.InsecureSkipVerify = true
	}
	client := &http.Client{
		Timeout:   5 * time.Second,
		Transport: &http.Transport{TLSClientConfig: tlsCfg},
	}
	resp, err := client.Get(fmt.Sprintf("https://%s/api/devices", cfg.ServerDomain))
	if err != nil {
		return fmt.Errorf("cannot reach server: %w", err)
	}
	defer resp.Body.Close()

	var peers []peerInfo
	if err := json.NewDecoder(resp.Body).Decode(&peers); err != nil {
		return fmt.Errorf("parse response: %w", err)
	}

	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintf(tw, "NAME\tIP\tSTATUS\n")
	for _, p := range peers {
		status := "offline"
		if p.Online {
			status = "online"
		}
		marker := ""
		if p.IP == cfg.DeviceIP {
			marker = " (me)"
		}
		fmt.Fprintf(tw, "%s%s\t%s\t%s\n", p.Name, marker, p.IP, status)
	}
	tw.Flush()
	return nil
}
