package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/maxyu/mesh/internal/client"
	"github.com/maxyu/mesh/internal/common/version"
)

func main() {
	root := &cobra.Command{
		Use:     "mesh",
		Short:   "Mesh VPN 客户端",
		Long:    "Mesh VPN 客户端 — 加入、启动、查看状态或退出 mesh 网络。",
		Version: version.Get(),
	}
	root.AddCommand(joinCmd(), upCmd(), statusCmd(), peersCmd(), leaveCmd(), versionCmd())
	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}

func joinCmd() *cobra.Command {
	var tok string
	var insecure bool
	cmd := &cobra.Command{
		Use:   "join <domain>",
		Args:  cobra.ExactArgs(1),
		Short: "向服务器注册并加入 mesh 网络（无需 root）",
		RunE: func(c *cobra.Command, args []string) error {
			if err := client.Join(args[0], tok, insecure); err != nil {
				return err
			}
			fmt.Println("Now run 'sudo mesh up' to start the tunnel.")
			return nil
		},
	}
	cmd.Flags().StringVar(&tok, "token", "", "注册令牌（由服务器管理员提供）")
	cmd.Flags().BoolVar(&insecure, "insecure", false, "跳过 TLS 证书校验（仅用于 e2e 测试）")
	_ = cmd.MarkFlagRequired("token")
	return cmd
}

func upCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "up",
		Short: "启动 VPN 隧道（需要 root 权限）",
		RunE: func(c *cobra.Command, args []string) error {
			// 同时监听 SIGHUP：用户在前台终端运行 `sudo mesh up` 后关闭终端
			// 窗口时，内核会向前台进程组发 SIGHUP；不监听会导致 mesh 进程
			// 残留（孤儿化）继续持有 TUN 设备和 WebSocket 连接。
			ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
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

func peersCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "peers",
		Short: "查看网络中的所有设备",
		RunE: func(c *cobra.Command, args []string) error {
			return client.Peers()
		},
	}
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
