package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/maxyu/mesh/internal/client"
)

func main() {
	root := &cobra.Command{
		Use:   "mesh",
		Short: "Mesh VPN 客户端",
		Long:  "Mesh VPN 客户端 — 加入、启动、查看状态或退出 mesh 网络。",
	}
	root.AddCommand(joinCmd(), upCmd(), statusCmd(), leaveCmd())
	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}

func joinCmd() *cobra.Command {
	var tok string
	cmd := &cobra.Command{
		Use:   "join <domain>",
		Args:  cobra.ExactArgs(1),
		Short: "向服务器注册并加入 mesh 网络（无需 root）",
		RunE: func(c *cobra.Command, args []string) error {
			if err := client.Join(args[0], tok); err != nil {
				return err
			}
			fmt.Println("Now run 'sudo mesh up' to start the tunnel.")
			return nil
		},
	}
	cmd.Flags().StringVar(&tok, "token", "", "注册令牌（由服务器管理员提供）")
	_ = cmd.MarkFlagRequired("token")
	return cmd
}

func upCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "up",
		Short: "启动 VPN 隧道（需要 root 权限）",
		RunE: func(c *cobra.Command, args []string) error {
			ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
			defer cancel()
			return client.Up(ctx)
		},
	}
}

func statusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "查看当前注册状态",
		RunE: func(c *cobra.Command, args []string) error {
			return client.Status()
		},
	}
}

func leaveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "leave",
		Short: "离开 mesh 网络并删除本地配置",
		RunE: func(c *cobra.Command, args []string) error {
			return client.Leave()
		},
	}
}
