package tunnel

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sync/atomic"
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
	statusDir string

	connected   atomic.Int32
	connectedAt atomic.Int64 // unix nano
	txPackets   atomic.Uint64
	txBytes     atomic.Uint64
	rxPackets   atomic.Uint64
	rxBytes     atomic.Uint64
	lastActive  atomic.Int64 // unix nano
	lastRTT     atomic.Int64 // microseconds
}

// NewTunnelClient creates a TunnelClient, initializes the TUN device, and
// configures the network interface.
func NewTunnelClient(serverURL, secret, localIP, network string, mtu int, statusDir string) (*TunnelClient, error) {
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
		statusDir: statusDir,
	}, nil
}

// Run blocks and continuously maintains a connection to the server.
// On connection loss it waits 3 seconds and reconnects. Returns when ctx
// is cancelled.
func (tc *TunnelClient) Run(ctx context.Context) error {
	go tc.writeStatusLoop(ctx)
	for {
		err := tc.connect(ctx)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		tc.connected.Store(0)
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

	tc.connected.Store(1)
	tc.connectedAt.Store(time.Now().UnixNano())
	log.Printf("connected to %s", tc.serverURL)

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Ping loop: measure RTT and detect dead connections.
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				start := time.Now()
				err := conn.Ping(ctx)
				if err != nil {
					cancel()
					return
				}
				tc.lastRTT.Store(time.Since(start).Microseconds())
			}
		}
	}()

	// WS → TUN: goroutine reads packets from WebSocket and writes to TUN.
	go func() {
		defer cancel()
		for {
			_, pkt, err := conn.Read(ctx)
			if err != nil {
				return
			}
			tc.rxPackets.Add(1)
			tc.rxBytes.Add(uint64(len(pkt)))
			tc.lastActive.Store(time.Now().UnixNano())

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

		pkt := make([]byte, sizes[0])
		copy(pkt, bufs[0][meshtun.Offset():meshtun.Offset()+sizes[0]])

		if err := conn.Write(ctx, websocket.MessageBinary, pkt); err != nil {
			return fmt.Errorf("write WS: %w", err)
		}
		tc.txPackets.Add(1)
		tc.txBytes.Add(uint64(len(pkt)))
		tc.lastActive.Store(time.Now().UnixNano())
	}
}

// writeStatusLoop periodically writes runtime stats to a JSON file.
func (tc *TunnelClient) writeStatusLoop(ctx context.Context) {
	path := filepath.Join(tc.statusDir, "status.json")
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	defer os.Remove(path)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			tc.writeStatus(path)
		}
	}
}

func (tc *TunnelClient) writeStatus(path string) {
	var connAt time.Time
	if ns := tc.connectedAt.Load(); ns > 0 {
		connAt = time.Unix(0, ns)
	}
	var lastAct time.Time
	if ns := tc.lastActive.Load(); ns > 0 {
		lastAct = time.Unix(0, ns)
	}
	stats := ClientStats{
		Connected:   tc.connected.Load() == 1,
		ConnectedAt: connAt,
		TxPackets:   tc.txPackets.Load(),
		TxBytes:     tc.txBytes.Load(),
		RxPackets:   tc.rxPackets.Load(),
		RxBytes:     tc.rxBytes.Load(),
		LastRTT:     tc.lastRTT.Load(),
		LastActive:  lastAct,
		PID:         os.Getpid(),
		UpdatedAt:   time.Now(),
	}
	data, _ := json.MarshalIndent(stats, "", "  ")
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return
	}
	os.Rename(tmp, path)
}

// Close shuts down the TUN device.
func (tc *TunnelClient) Close() error {
	return tc.tun.Close()
}
