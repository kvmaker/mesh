package main

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"github.com/maxyu/mesh/internal/api"
	"github.com/maxyu/mesh/internal/config"
	"github.com/maxyu/mesh/internal/db"
	"github.com/maxyu/mesh/internal/device"
	"github.com/maxyu/mesh/internal/token"
	"github.com/maxyu/mesh/internal/wg"
)

var cfgPath string

func main() {
	root := &cobra.Command{
		Use:   "meshd",
		Short: "Mesh VPN server daemon",
	}
	root.PersistentFlags().StringVar(&cfgPath, "config", "/etc/mesh/meshd.yaml", "config file path")

	root.AddCommand(initCmd())
	root.AddCommand(runCmd())
	root.AddCommand(tokenCmd())
	root.AddCommand(deviceCmd())
	root.AddCommand(statusCmd())

	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}

func loadConfig() *config.Config {
	cfg, err := config.Load(cfgPath)
	if err != nil {
		cfg = config.Default()
	}
	return cfg
}

func openDB(cfg *config.Config) *sql.DB {
	dbPath := filepath.Join(cfg.DataDir, "mesh.db")
	database, err := db.Open(dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error opening database: %v\n", err)
		os.Exit(1)
	}
	return database
}

func initCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Initialize server (generate keys, token, database)",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := loadConfig()

			// 创建数据目录
			if err := os.MkdirAll(cfg.DataDir, 0700); err != nil {
				return fmt.Errorf("create data dir: %w", err)
			}

			// 生成 WireGuard 密钥对
			keyPath := filepath.Join(cfg.DataDir, "server.key")
			pubPath := filepath.Join(cfg.DataDir, "server.pub")
			if _, err := os.Stat(keyPath); os.IsNotExist(err) {
				priv, pub, err := wg.GenerateKeyPair()
				if err != nil {
					return fmt.Errorf("generate keys: %w", err)
				}
				if err := os.WriteFile(keyPath, []byte(priv), 0600); err != nil {
					return err
				}
				if err := os.WriteFile(pubPath, []byte(pub), 0644); err != nil {
					return err
				}
				fmt.Printf("Server keys generated\n")
			} else {
				fmt.Printf("Server keys already exist, skipping\n")
			}

			// 初始化数据库
			database := openDB(cfg)
			defer database.Close()
			if err := db.Migrate(database); err != nil {
				return fmt.Errorf("migrate: %w", err)
			}

			// 生成 Token
			tok, err := token.Generate()
			if err != nil {
				return fmt.Errorf("generate token: %w", err)
			}
			if err := token.Save(database, tok); err != nil {
				return fmt.Errorf("save token: %w", err)
			}

			// 保存 token 到文件
			tokenPath := filepath.Join(cfg.DataDir, "token")
			_ = os.WriteFile(tokenPath, []byte(tok), 0600)

			fmt.Printf("Initialization complete.\n")
			fmt.Printf("Token: %s\n", tok)
			fmt.Printf("Data dir: %s\n", cfg.DataDir)
			return nil
		},
	}
}

func runCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "run",
		Short: "Start the meshd daemon",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := loadConfig()
			database := openDB(cfg)
			defer database.Close()
			_ = db.Migrate(database)

			// 读取服务器私钥
			keyPath := filepath.Join(cfg.DataDir, "server.key")
			privKey, err := os.ReadFile(keyPath)
			if err != nil {
				return fmt.Errorf("read server key: %w", err)
			}

			// 设置 WireGuard 接口
			if err := wg.Setup(wg.SetupConfig{
				Interface:  "wg0",
				ListenPort: cfg.ListenPort,
				PrivateKey: strings.TrimSpace(string(privKey)),
				Address:    "10.100.0.1/24",
			}); err != nil {
				return fmt.Errorf("setup wireguard: %w", err)
			}
			fmt.Printf("WireGuard interface wg0 configured\n")

			// 恢复已注册的 peers
			devices, _ := device.List(database)
			for _, d := range devices {
				_ = wg.AddPeer("wg0", d.PublicKey, d.IP)
			}
			fmt.Printf("Restored %d peers\n", len(devices))

			// 启动离线检测 goroutine
			go func() {
				ticker := time.NewTicker(30 * time.Second)
				for range ticker.C {
					_ = device.MarkOffline(database, 90*time.Second)
				}
			}()

			// 启动 API 服务
			srv := api.New(database, cfg, "wg0")
			fmt.Printf("API listening on :%d\n", cfg.APIPort)
			return srv.ListenAndServe()
		},
	}
}

func tokenCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "token",
		Short: "Manage registration token",
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "show",
		Short: "Show current token",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := loadConfig()
			database := openDB(cfg)
			defer database.Close()

			tok, err := token.Load(database)
			if err != nil {
				return fmt.Errorf("no token found, run 'meshd init' first")
			}
			fmt.Println(tok)
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "reset",
		Short: "Generate a new token (invalidates the old one)",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := loadConfig()
			database := openDB(cfg)
			defer database.Close()

			tok, err := token.Generate()
			if err != nil {
				return err
			}
			if err := token.Save(database, tok); err != nil {
				return err
			}
			tokenPath := filepath.Join(cfg.DataDir, "token")
			_ = os.WriteFile(tokenPath, []byte(tok), 0600)
			fmt.Printf("New token: %s\n", tok)
			return nil
		},
	})

	return cmd
}

func deviceCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "device",
		Short: "Manage devices",
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List all devices",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := loadConfig()
			database := openDB(cfg)
			defer database.Close()

			devices, err := device.List(database)
			if err != nil {
				return err
			}

			tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintf(tw, "ID\tNAME\tIP\tONLINE\tPASSIVE\tLAST SEEN\n")
			for _, d := range devices {
				online := "no"
				if d.Online {
					online = "yes"
				}
				passive := ""
				if d.Passive {
					passive = "yes"
				}
				lastSeen := "-"
				if !d.LastSeen.IsZero() {
					lastSeen = d.LastSeen.Format("2006-01-02 15:04:05")
				}
				idShort := d.ID
				if len(idShort) > 8 {
					idShort = idShort[:8]
				}
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n",
					idShort, d.Name, d.IP, online, passive, lastSeen)
			}
			tw.Flush()
			return nil
		},
	})

	removeCmd := &cobra.Command{
		Use:   "remove <name|id>",
		Short: "Remove a device",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := loadConfig()
			database := openDB(cfg)
			defer database.Close()

			// 查找设备（按 name 或 id 前缀）
			devices, _ := device.List(database)
			var target *device.Device
			for i, d := range devices {
				if d.Name == args[0] || strings.HasPrefix(d.ID, args[0]) {
					target = &devices[i]
					break
				}
			}
			if target == nil {
				return fmt.Errorf("device %q not found", args[0])
			}

			_ = wg.RemovePeer("wg0", target.PublicKey)
			if err := device.Delete(database, target.ID); err != nil {
				return err
			}
			fmt.Printf("Removed device %s (%s)\n", target.Name, target.IP)
			return nil
		},
	}
	cmd.AddCommand(removeCmd)

	addCmd := &cobra.Command{
		Use:   "add <name>",
		Short: "Add a passive device (generates config + QR code)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			passive, _ := cmd.Flags().GetBool("passive")
			if !passive {
				return fmt.Errorf("use 'mesh join' for active devices, or add --passive for passive devices")
			}

			cfg := loadConfig()
			database := openDB(cfg)
			defer database.Close()

			// 生成密钥对
			privKey, pubKey, err := wg.GenerateKeyPair()
			if err != nil {
				return err
			}

			// 分配 IP
			ip, err := device.Allocate(database, cfg.Network)
			if err != nil {
				return err
			}

			// 生成 secret
			secretBytes := make([]byte, 32)
			if _, err := rand.Read(secretBytes); err != nil {
				return fmt.Errorf("generate secret: %w", err)
			}
			secret := hex.EncodeToString(secretBytes)

			// 释放临时占位记录，再创建正式设备记录
			device.Release(database, ip)
			d := &device.Device{
				ID:        uuid.New().String(),
				Name:      args[0],
				PublicKey: pubKey,
				IP:        ip,
				Secret:    secret,
				Passive:   true,
			}
			if err := device.Create(database, d); err != nil {
				return err
			}

			// 添加 WireGuard peer
			_ = wg.AddPeer("wg0", pubKey, ip)

			// 读取服务器公钥
			pubPath := filepath.Join(cfg.DataDir, "server.pub")
			serverPub, _ := os.ReadFile(pubPath)

			// 生成配置文件内容
			conf := fmt.Sprintf(`[Interface]
PrivateKey = %s
Address = %s/24
DNS = 1.1.1.1

[Peer]
PublicKey = %s
Endpoint = %s
AllowedIPs = 10.100.0.0/24
PersistentKeepalive = 25
`, privKey, ip, strings.TrimSpace(string(serverPub)), cfg.Endpoint)

			fmt.Println("=== WireGuard Configuration ===")
			fmt.Println(conf)

			// 生成终端二维码
			qrCmd := exec.Command("qrencode", "-t", "ansiutf8")
			qrCmd.Stdin = strings.NewReader(conf)
			qrCmd.Stdout = os.Stdout
			if err := qrCmd.Run(); err != nil {
				fmt.Println("(Install qrencode for QR code display: apt install qrencode)")
			}

			return nil
		},
	}
	addCmd.Flags().Bool("passive", false, "Create a passive device")
	cmd.AddCommand(addCmd)

	cmd.AddCommand(&cobra.Command{
		Use:   "show <name>",
		Short: "Show passive device config and QR code",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// 被动设备私钥不持久存储，仅在 add 时输出
			return fmt.Errorf("private key is only shown at creation time; use 'meshd device remove' and 'meshd device add --passive' to regenerate")
		},
	})

	return cmd
}

func statusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show WireGuard peer status",
		RunE: func(cmd *cobra.Command, args []string) error {
			peers, err := wg.Show("wg0")
			if err != nil {
				return err
			}

			tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintf(tw, "PUBLIC KEY\tALLOWED IPS\tLAST HANDSHAKE\tTRANSFER\n")
			for _, p := range peers {
				pubShort := p.PublicKey
				if len(pubShort) > 8 {
					pubShort = pubShort[:8]
				}
				fmt.Fprintf(tw, "%s...\t%s\t%s\t↓%s ↑%s\n",
					pubShort, p.AllowedIPs, p.LastHandshake, p.TransferRx, p.TransferTx)
			}
			tw.Flush()
			return nil
		},
	}
}
