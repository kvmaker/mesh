package main

import (
	"context"
	"crypto/tls"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/dustin/go-humanize"
	"github.com/spf13/cobra"

	"github.com/maxyu/mesh/internal/api"
	"github.com/maxyu/mesh/internal/config"
	"github.com/maxyu/mesh/internal/db"
	"github.com/maxyu/mesh/internal/device"
	"github.com/maxyu/mesh/internal/token"
	"github.com/maxyu/mesh/internal/tunnel"
)

var cfgPath string

func main() {
	root := &cobra.Command{Use: "meshd", Short: "Mesh VPN server"}
	root.PersistentFlags().StringVar(&cfgPath, "config", "/etc/mesh/meshd.yaml", "config file")
	root.AddCommand(initCmd(), runCmd(), tokenCmd(), deviceCmd())
	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}

func loadCfg() *config.Config {
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return config.Default()
	}
	return cfg
}

func openDB(cfg *config.Config) *sql.DB {
	d, err := db.Open(filepath.Join(cfg.DataDir, "mesh.db"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "db error: %v\n", err)
		os.Exit(1)
	}
	return d
}

func initCmd() *cobra.Command {
	return &cobra.Command{
		Use: "init", Short: "Initialize server",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := loadCfg()
			os.MkdirAll(cfg.DataDir, 0700)
			os.MkdirAll(cfg.CertDir, 0700)
			d := openDB(cfg)
			defer d.Close()
			db.Migrate(d)
			tok, _ := token.Generate()
			token.Save(d, tok)
			fmt.Printf("Initialized.\nToken: %s\n", tok)
			return nil
		},
	}
}

func runCmd() *cobra.Command {
	return &cobra.Command{
		Use: "run", Short: "Start server",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := loadCfg()
			d := openDB(cfg)
			defer d.Close()
			db.Migrate(d)

			ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
			defer cancel()

			ts, err := tunnel.NewTunnelServer(d, cfg)
			if err != nil {
				return fmt.Errorf("tunnel server: %w", err)
			}
			defer ts.Close()
			ts.Start(ctx)

			srv := api.New(d, cfg, ts)
			fmt.Printf("Mesh VPN server starting on %s (domain: %s)\n", cfg.ListenAddr, cfg.Domain)
			return srv.ListenAndServeTLS(ctx)
		},
	}
}

func tokenCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "token", Short: "Manage token"}
	cmd.AddCommand(
		&cobra.Command{Use: "show", RunE: func(c *cobra.Command, a []string) error {
			cfg := loadCfg()
			d := openDB(cfg)
			defer d.Close()
			tok, err := token.Load(d)
			if err != nil {
				return err
			}
			fmt.Println(tok)
			return nil
		}},
		&cobra.Command{Use: "reset", RunE: func(c *cobra.Command, a []string) error {
			cfg := loadCfg()
			d := openDB(cfg)
			defer d.Close()
			tok, _ := token.Generate()
			token.Save(d, tok)
			fmt.Printf("New token: %s\n", tok)
			return nil
		}},
	)
	return cmd
}

func deviceCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "device", Short: "Manage devices"}

	listCmd := &cobra.Command{Use: "list", Short: "List all devices", RunE: func(c *cobra.Command, a []string) error {
		showStats, _ := c.Flags().GetBool("stats")
		cfg := loadCfg()
		d := openDB(cfg)
		defer d.Close()
		devs, _ := device.List(d)

		var statsMap map[string]tunnel.ConnStats
		if showStats {
			statsMap = fetchRuntimeStats(cfg.Domain)
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
		cfg := loadCfg()
		d := openDB(cfg)
		defer d.Close()
		devs, _ := device.List(d)
		for _, dev := range devs {
			if dev.Name == a[0] || strings.HasPrefix(dev.ID, a[0]) {
				device.Delete(d, dev.ID)
				fmt.Printf("Removed %s (%s)\n", dev.Name, dev.IP)
				return nil
			}
		}
		return fmt.Errorf("device %q not found", a[0])
	}})

	return cmd
}

func fetchRuntimeStats(domain string) map[string]tunnel.ConnStats {
	client := &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}
	resp, err := client.Get(fmt.Sprintf("https://%s/api/stats/devices", domain))
	if err != nil {
		return nil
	}
	defer resp.Body.Close()

	var stats []tunnel.ConnStats
	if err := json.NewDecoder(resp.Body).Decode(&stats); err != nil {
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
