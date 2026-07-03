package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"text/tabwriter"

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
	cmd.AddCommand(
		&cobra.Command{Use: "list", RunE: func(c *cobra.Command, a []string) error {
			cfg := loadCfg()
			d := openDB(cfg)
			defer d.Close()
			devs, _ := device.List(d)
			tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintf(tw, "ID\tNAME\tIP\tONLINE\tLAST SEEN\n")
			for _, dev := range devs {
				online := "no"
				if dev.Online {
					online = "yes"
				}
				lastSeen := "-"
				if !dev.LastSeen.IsZero() {
					lastSeen = dev.LastSeen.Format("2006-01-02 15:04:05")
				}
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", dev.ID[:8], dev.Name, dev.IP, online, lastSeen)
			}
			tw.Flush()
			return nil
		}},
		&cobra.Command{Use: "remove <name|id>", Args: cobra.ExactArgs(1), RunE: func(c *cobra.Command, a []string) error {
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
		}},
	)
	return cmd
}
