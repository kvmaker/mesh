package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/maxyu/mesh/internal/client"
)

func main() {
	root := &cobra.Command{
		Use:   "mesh",
		Short: "Mesh VPN client",
		Long:  "Mesh VPN client — join, manage, and leave a WireGuard-based mesh network.",
	}

	root.AddCommand(joinCmd())
	root.AddCommand(statusCmd())
	root.AddCommand(leaveCmd())

	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}

func joinCmd() *cobra.Command {
	var token string

	cmd := &cobra.Command{
		Use:   "join <server:port>",
		Short: "Join a mesh network",
		Long:  "Register this device with the mesh server and configure the local WireGuard interface.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if token == "" {
				return fmt.Errorf("--token is required")
			}
			if err := client.Join(args[0], token); err != nil {
				return err
			}
			fmt.Println("Press Ctrl+C to disconnect.")
			select {}
		},
	}

	cmd.Flags().StringVar(&token, "token", "", "Registration token (required)")
	_ = cmd.MarkFlagRequired("token")

	return cmd
}

func statusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show connection status",
		Long:  "Display current server address, assigned IP, and WireGuard connection status.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return client.Status()
		},
	}
}

func leaveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "leave",
		Short: "Leave the mesh network",
		Long:  "Deregister this device from the server, tear down WireGuard, and remove local config.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return client.Leave()
		},
	}
}
