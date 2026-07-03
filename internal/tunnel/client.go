package tunnel

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/coder/websocket"
	meshtun "github.com/maxyu/mesh/internal/tun"
)

// TunnelClient connects to the mesh VPN server via WebSocket and shuttles
// IP packets through a local TUN device.
type TunnelClient struct {
	serverURL string
	secret    string
	mtu       int
	tun       meshtun.Device
}

// NewTunnelClient creates a TunnelClient, initializes the TUN device, and
// configures the network interface.
func NewTunnelClient(serverURL, secret, localIP, network string, mtu int) (*TunnelClient, error) {
	tunName := meshtun.DefaultTUNName()
	dev, name, err := meshtun.CreateTUN(tunName, mtu)
	if err != nil {
		return nil, err
	}
	if err := meshtun.ConfigureInterface(name, localIP, network); err != nil {
		dev.Close()
		return nil, err
	}
	return &TunnelClient{
		serverURL: serverURL,
		secret:    secret,
		mtu:       mtu,
		tun:       dev,
	}, nil
}

// Run blocks and continuously maintains a connection to the server.
// On connection loss it waits 3 seconds and reconnects. Returns when ctx
// is cancelled.
func (tc *TunnelClient) Run(ctx context.Context) error {
	for {
		err := tc.connect(ctx)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		log.Printf("connection lost: %v, reconnecting in 3s...", err)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(3 * time.Second):
		}
	}
}

// connect dials the WebSocket server and runs the bidirectional packet loop
// until either the context is cancelled or an error occurs.
func (tc *TunnelClient) connect(ctx context.Context) error {
	header := http.Header{}
	header.Set("Authorization", "Bearer "+tc.secret)

	conn, _, err := websocket.Dial(ctx, tc.serverURL, &websocket.DialOptions{
		HTTPHeader: header,
	})
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	defer conn.CloseNow()

	log.Printf("connected to %s", tc.serverURL)

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// WS → TUN: goroutine reads packets from WebSocket and writes to TUN.
	go func() {
		defer cancel()
		for {
			_, pkt, err := conn.Read(ctx)
			if err != nil {
				return
			}
			// Prepend 4-byte offset for TUN header (required on macOS utun)
			buf := make([]byte, meshtun.Offset()+len(pkt))
			copy(buf[meshtun.Offset():], pkt)
			bufs := [][]byte{buf}
			if _, err := tc.tun.Write(bufs, meshtun.Offset()); err != nil {
				log.Printf("write to TUN: %v", err)
			}
		}
	}()

	// TUN → WS: main loop reads packets from TUN and sends over WebSocket.
	bufs := make([][]byte, 1)
	bufs[0] = make([]byte, meshtun.Offset()+tc.mtu+100)
	sizes := make([]int, 1)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		n, err := tc.tun.Read(bufs, sizes, meshtun.Offset())
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("read TUN: %w", err)
		}
		if n == 0 {
			continue
		}

		// Packet data starts at offset 4
		pkt := make([]byte, sizes[0])
		copy(pkt, bufs[0][meshtun.Offset():meshtun.Offset()+sizes[0]])

		if err := conn.Write(ctx, websocket.MessageBinary, pkt); err != nil {
			return fmt.Errorf("write WS: %w", err)
		}
	}
}

// Close shuts down the TUN device.
func (tc *TunnelClient) Close() error {
	return tc.tun.Close()
}
