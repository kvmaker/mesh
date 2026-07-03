package tunnel

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/netip"
	"strings"

	"github.com/coder/websocket"
	"github.com/maxyu/mesh/internal/config"
	"github.com/maxyu/mesh/internal/device"
	meshtun "github.com/maxyu/mesh/internal/tun"
)

// TunnelServer manages TUN device and WebSocket connections for the mesh VPN.
type TunnelServer struct {
	db      *sql.DB
	cfg     *config.Config
	tun     meshtun.Device
	tunName string
	router  *Router
	tunIP   netip.Addr
}

// serverIP derives the server's VPN IP from the network CIDR (first usable host).
func serverIP(network string) (string, error) {
	ip, _, err := net.ParseCIDR(network)
	if err != nil {
		return "", fmt.Errorf("parse network: %w", err)
	}
	v4 := ip.To4()
	if v4 == nil {
		return "", fmt.Errorf("only IPv4 supported")
	}
	v4[3] = 1
	return v4.String(), nil
}

// NewTunnelServer creates a TunnelServer, initializes the TUN device, and configures the network interface.
func NewTunnelServer(db *sql.DB, cfg *config.Config) (*TunnelServer, error) {
	srvIP, err := serverIP(cfg.Network)
	if err != nil {
		return nil, err
	}
	dev, name, err := meshtun.CreateTUN(cfg.TunName, cfg.TunMTU)
	if err != nil {
		return nil, err
	}
	if err := meshtun.ConfigureInterface(name, srvIP, cfg.Network); err != nil {
		dev.Close()
		return nil, err
	}
	return &TunnelServer{
		db:      db,
		cfg:     cfg,
		tun:     dev,
		tunName: name,
		router:  NewRouter(),
		tunIP:   netip.MustParseAddr(srvIP),
	}, nil
}

// Start launches the TUN read loop in a goroutine.
func (ts *TunnelServer) Start(ctx context.Context) {
	go ts.readTUN(ctx)
}

// readTUN reads packets from the TUN device and routes them to the appropriate WebSocket client.
func (ts *TunnelServer) readTUN(ctx context.Context) {
	bufs := make([][]byte, 1)
	bufs[0] = make([]byte, meshtun.Offset()+ts.cfg.TunMTU+100)
	sizes := make([]int, 1)

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		n, err := ts.tun.Read(bufs, sizes, meshtun.Offset())
		if err != nil || n == 0 {
			continue
		}
		pkt := make([]byte, sizes[0])
		copy(pkt, bufs[0][meshtun.Offset():meshtun.Offset()+sizes[0]])
		ts.routePacket(pkt)
	}
}

// routePacket routes a packet from the TUN device to the correct WebSocket connection.
func (ts *TunnelServer) routePacket(pkt []byte) {
	dst, err := ExtractDstIP(pkt)
	if err != nil {
		return
	}
	cc, ok := ts.router.Lookup(dst)
	if !ok {
		return
	}
	if err := cc.Conn.Write(context.Background(), websocket.MessageBinary, pkt); err != nil {
		log.Printf("write to client %s: %v", cc.DeviceID, err)
	}
}

// HandleWebSocket handles incoming WebSocket upgrade requests, authenticates the device,
// registers it in the router, and forwards packets bidirectionally.
func (ts *TunnelServer) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	secret := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if secret == "" {
		secret = r.URL.Query().Get("token")
	}
	if secret == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	dev, err := device.GetBySecret(ts.db, secret)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{})
	if err != nil {
		return
	}
	defer conn.CloseNow()

	ip, err := netip.ParseAddr(dev.IP)
	if err != nil {
		log.Printf("invalid IP for device %s: %v", dev.ID, err)
		return
	}

	cc := &ClientConn{Conn: conn, DeviceID: dev.ID, IP: ip}
	ts.router.Register(ip, cc)
	defer ts.router.Unregister(ip)

	if err := device.UpdateOnline(ts.db, dev.ID, true); err != nil {
		log.Printf("update online status for %s: %v", dev.ID, err)
	}
	defer func() {
		if err := device.UpdateOnline(ts.db, dev.ID, false); err != nil {
			log.Printf("update offline status for %s: %v", dev.ID, err)
		}
	}()

	log.Printf("device %s (%s) connected", dev.Name, dev.IP)
	defer log.Printf("device %s (%s) disconnected", dev.Name, dev.IP)

	ctx := r.Context()
	ts.clientReadLoop(ctx, conn)
}

// clientReadLoop reads packets from a WebSocket client and routes them appropriately.
// Packets destined for the server IP (10.100.0.1) are injected into the TUN device;
// packets destined for other clients are forwarded directly via WS→WS routing.
func (ts *TunnelServer) clientReadLoop(ctx context.Context, conn *websocket.Conn) {
	for {
		_, pkt, err := conn.Read(ctx)
		if err != nil {
			return
		}

		dst, err := ExtractDstIP(pkt)
		if err != nil {
			log.Printf("packet parse error: %v (len=%d)", err, len(pkt))
			continue
		}

		log.Printf("packet: len=%d dst=%s", len(pkt), dst)

		if dst == ts.tunIP {
			buf := make([]byte, meshtun.Offset()+len(pkt))
			copy(buf[meshtun.Offset():], pkt)
			bufs := [][]byte{buf}
			if _, err := ts.tun.Write(bufs, meshtun.Offset()); err != nil {
				log.Printf("write to TUN: %v", err)
			}
		} else if cc, ok := ts.router.Lookup(dst); ok {
			if err := cc.Conn.Write(ctx, websocket.MessageBinary, pkt); err != nil {
				log.Printf("forward to client %s: %v", cc.DeviceID, err)
			}
		} else {
			log.Printf("no route for %s", dst)
		}
	}
}

// Close shuts down the TUN device.
func (ts *TunnelServer) Close() error {
	return ts.tun.Close()
}
