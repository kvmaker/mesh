package server

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/maxyu/mesh/internal/common/version"
	"github.com/maxyu/mesh/internal/server/api"
	"github.com/maxyu/mesh/internal/server/config"
	"github.com/maxyu/mesh/internal/server/db"
	"github.com/maxyu/mesh/internal/server/token"
	"github.com/maxyu/mesh/internal/server/tunnel"
)

// cfgPath 由 root 命令的 --config 持久化 flag 绑定，各子命令通过 loadCfg 读取。
var cfgPath string

// NewRootCmd 构建 meshd 的根命令，注册 --config 持久化 flag 与全部子命令。
// cmd/meshd 只需 NewRootCmd().Execute()，业务逻辑全部收敛在本包。
func NewRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:     "meshd",
		Short:   "Mesh VPN server",
		Version: version.Get(),
	}
	root.PersistentFlags().StringVar(&cfgPath, "config", "/etc/mesh/meshd.yaml", "config file")
	root.AddCommand(initCmd(), runCmd(), tokenCmd(), deviceCmd(), versionCmd())
	return root
}

// loadCfg 读取 --config 指向的配置文件。文件不存在属正常情况（首次
// init 前配置尚未落盘），静默回退默认值；但文件存在却解析失败（YAML
// 语法错误等）说明用户配置有误，打印告警以免静默使用与预期不符的默认值。
func loadCfg() *config.Config {
	cfg, err := config.Load(cfgPath)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			fmt.Fprintf(os.Stderr, "warning: failed to load config %s: %v; using defaults\n", cfgPath, err)
		}
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
			if err := os.MkdirAll(cfg.DataDir, 0700); err != nil {
				return fmt.Errorf("create data dir: %w", err)
			}
			if err := os.MkdirAll(cfg.CertDir, 0700); err != nil {
				return fmt.Errorf("create cert dir: %w", err)
			}
			d := openDB(cfg)
			defer d.Close()
			if err := db.Migrate(d); err != nil {
				return fmt.Errorf("migrate db: %w", err)
			}
			tok, err := token.Generate()
			if err != nil {
				return fmt.Errorf("generate token: %w", err)
			}
			if err := token.Save(d, tok); err != nil {
				return fmt.Errorf("save token: %w", err)
			}
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
			if err := db.Migrate(d); err != nil {
				return fmt.Errorf("migrate db: %w", err)
			}

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
			tok, err := token.Generate()
			if err != nil {
				return fmt.Errorf("generate token: %w", err)
			}
			if err := token.Save(d, tok); err != nil {
				return fmt.Errorf("save token: %w", err)
			}
			fmt.Printf("New token: %s\n", tok)
			return nil
		}},
	)
	return cmd
}

func versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "打印当前软件版本",
		Run: func(c *cobra.Command, args []string) {
			fmt.Println(version.Get())
		},
	}
}
