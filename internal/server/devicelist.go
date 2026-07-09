package server

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/dustin/go-humanize"
	"github.com/spf13/cobra"

	"github.com/maxyu/mesh/internal/server/device"
	"github.com/maxyu/mesh/internal/server/token"
	"github.com/maxyu/mesh/internal/server/tunnel"
)

func deviceCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "device", Short: "Manage devices"}

	listCmd := &cobra.Command{Use: "list", Short: "List all devices", RunE: func(c *cobra.Command, a []string) error {
		showStats, _ := c.Flags().GetBool("stats")
		cfg := loadCfg()
		d := openDB(cfg)
		defer d.Close()
		devs, err := device.List(d)
		if err != nil {
			return fmt.Errorf("list devices: %w", err)
		}

		var statsMap map[string]tunnel.ConnStats
		if showStats {
			// stats 接口需鉴权，用服务端管理 token 作为 Bearer 凭证。
			// token 未初始化时不阻断 list，仅退化为无统计列。
			authToken, _ := token.Load(d)
			statsMap = fetchRuntimeStats(cfg.Domain, authToken, cfg.TLSTestMode)
		}

		tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		if showStats {
			fmt.Fprintf(tw, "ID\tNAME\tIP\tONLINE\tDURATION\tTX\tRX\tLAST PKT\tLAST SEEN\n")
		} else {
			fmt.Fprintf(tw, "ID\tNAME\tIP\tONLINE\tLAST SEEN\n")
		}

		for _, dev := range devs {
			online := "no"
			if dev.Online {
				online = "yes"
			}
			lastSeen := "-"
			if !dev.LastSeen.IsZero() {
				lastSeen = dev.LastSeen.Format("2006-01-02 15:04:05")
			}

			if showStats {
				duration, tx, rx, lastPkt := "-", "-", "-", "-"
				if s, ok := statsMap[dev.ID]; ok {
					duration = formatDuration(time.Since(s.ConnectedAt))
					tx = fmt.Sprintf("%s/%s", humanize.Comma(int64(s.TxPackets)), humanize.IBytes(s.TxBytes))
					rx = fmt.Sprintf("%s/%s", humanize.Comma(int64(s.RxPackets)), humanize.IBytes(s.RxBytes))
					if !s.LastPacket.IsZero() {
						lastPkt = formatDuration(time.Since(s.LastPacket)) + " ago"
					}
				}
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
					dev.ID[:8], dev.Name, dev.IP, online, duration, tx, rx, lastPkt, lastSeen)
			} else {
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", dev.ID[:8], dev.Name, dev.IP, online, lastSeen)
			}
		}
		tw.Flush()
		return nil
	}}
	listCmd.Flags().BoolP("stats", "s", false, "Show runtime statistics (requires server running)")
	cmd.AddCommand(listCmd)

	cmd.AddCommand(&cobra.Command{Use: "remove <name|id>", Args: cobra.ExactArgs(1), RunE: func(c *cobra.Command, a []string) error {
		arg := strings.TrimSpace(a[0])
		if arg == "" {
			return fmt.Errorf("device identifier must not be empty")
		}
		cfg := loadCfg()
		d := openDB(cfg)
		defer d.Close()
		devs, err := device.List(d)
		if err != nil {
			return fmt.Errorf("list devices: %w", err)
		}

		// 精确匹配 name 优先；否则按 ID 前缀匹配。收集全部命中，
		// 前缀歧义（多于一个设备匹配）时报错而非静默删除首个，避免误删。
		var matches []device.Device
		for _, dev := range devs {
			if dev.Name == arg || strings.HasPrefix(dev.ID, arg) {
				matches = append(matches, dev)
			}
		}
		switch len(matches) {
		case 0:
			return fmt.Errorf("device %q not found", arg)
		case 1:
			dev := matches[0]
			if err := device.Delete(d, dev.ID); err != nil {
				return fmt.Errorf("remove device: %w", err)
			}
			fmt.Printf("Removed %s (%s)\n", dev.Name, dev.IP)
			return nil
		default:
			ids := make([]string, len(matches))
			for i, m := range matches {
				ids[i] = m.ID[:8]
			}
			return fmt.Errorf("%q matches multiple devices (%s); use a longer prefix or exact ID", arg, strings.Join(ids, ", "))
		}
	}})

	return cmd
}

// fetchRuntimeStats 向本机运行中的 meshd 拉取实时连接统计。
//
// authToken 作为 Authorization: Bearer 凭证（服务端管理 token），/api/stats
// 接口需鉴权。insecureTLS 仅在测试模式（cfg.TLSTestMode，由 MESH_TEST_TLS
// 控制、服务端使用自签证书）下为 true 以跳过证书校验；生产环境走系统默认
// 的完整证书校验，避免中间人攻击。拉取失败时打印告警并返回 nil，让
// device list 退化为不带统计列的输出，而非静默吞掉错误。
func fetchRuntimeStats(domain, authToken string, insecureTLS bool) map[string]tunnel.ConnStats {
	transport := &http.Transport{}
	if insecureTLS {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec // 仅测试模式自签证书
	}
	client := &http.Client{
		Timeout:   5 * time.Second,
		Transport: transport,
	}
	req, err := http.NewRequest("GET", fmt.Sprintf("https://%s/api/stats/devices", domain), nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to build stats request: %v\n", err)
		return nil
	}
	req.Header.Set("Authorization", "Bearer "+authToken)
	resp, err := client.Do(req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to fetch runtime stats: %v\n", err)
		return nil
	}
	defer resp.Body.Close()

	var stats []tunnel.ConnStats
	if err := json.NewDecoder(resp.Body).Decode(&stats); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to decode runtime stats: %v\n", err)
		return nil
	}
	m := make(map[string]tunnel.ConnStats, len(stats))
	for _, s := range stats {
		m[s.DeviceID] = s
	}
	return m
}

func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm%ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	return fmt.Sprintf("%dh%dm", h, m)
}
