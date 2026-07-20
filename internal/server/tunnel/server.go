package tunnel

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/netip"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
	meshtun "github.com/maxyu/mesh/internal/common/tun"
	"github.com/maxyu/mesh/internal/server/config"
	"github.com/maxyu/mesh/internal/server/device"
)

// debugPacket 控制是否在转发热路径逐包打印 route-miss / parse-error 日志。
// 由环境变量 MESH_DEBUG_PACKET 控制，默认关闭：VPN 高包量下逐包日志会带来
// 格式化 / 锁 / journald I/O 开销与延迟抖动。关闭时热路径只累加原子计数器，
// 由 statsLoop 周期性聚合输出。排障时临时 export MESH_DEBUG_PACKET=1 即可。
var debugPacket = func() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("MESH_DEBUG_PACKET"))) {
	case "1", "true", "on", "yes":
		return true
	}
	return false
}()

// statsInterval 是聚合统计日志的输出周期。
const statsInterval = 30 * time.Second

// TunnelServer manages TUN device and WebSocket connections for the mesh VPN.
type TunnelServer struct {
	db      *sql.DB
	cfg     *config.Config
	tun     meshtun.Device
	tunName string
	router  *Router
	tunIP   netip.Addr

	// 热路径计数器：替代逐包日志。routeMiss 统计找不到路由的包，
	// parseErr 统计无法解析目的 IP 的畸形包。由 statsLoop 周期性聚合输出。
	routeMiss atomic.Uint64
	parseErr  atomic.Uint64
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

// NewTunnelServer creates a TunnelServer. In full mode it initializes the TUN
// device and configures the network interface so the server itself joins the
// VPN subnet. In relay mode it skips TUN creation entirely — the server acts
// purely as a packet relay between clients and needs no CAP_NET_ADMIN.
func NewTunnelServer(db *sql.DB, cfg *config.Config) (*TunnelServer, error) {
	ts := &TunnelServer{
		db:     db,
		cfg:    cfg,
		router: NewRouter(),
	}
	if cfg.Mode == config.ModeRelay {
		// tun / tunName / tunIP 保持零值:tun==nil 即 relay 标志,
		// Start / routeClientPacket / Close 据此走 nil 守卫分支。
		return ts, nil
	}

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
	ts.tun = dev
	ts.tunName = name
	ts.tunIP = netip.MustParseAddr(srvIP)
	return ts, nil
}

// Start launches the TUN read loop (full mode only) and the periodic stats
// aggregation loop. In relay mode there is no TUN to read.
func (ts *TunnelServer) Start(ctx context.Context) {
	if ts.tun != nil {
		go ts.readTUN(ctx)
	}
	go ts.statsLoop(ctx)
}

// statsLoop periodically emits an aggregated line for hot-path drop counters
// (route misses and parse errors) instead of logging them per packet. It only
// prints when there is new activity since the last tick, so an idle server
// stays quiet. Exits when ctx is cancelled.
func (ts *TunnelServer) statsLoop(ctx context.Context) {
	ticker := time.NewTicker(statsInterval)
	defer ticker.Stop()

	var lastMiss, lastParse uint64
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			miss := ts.routeMiss.Load()
			parse := ts.parseErr.Load()
			if miss == lastMiss && parse == lastParse {
				continue
			}
			log.Printf("[stats] route_miss=%d (+%d) parse_err=%d (+%d)",
				miss, miss-lastMiss, parse, parse-lastParse)
			lastMiss, lastParse = miss, parse
		}
	}
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
		if err != nil {
			// TUN 读出错（含设备关闭）不立即重试，否则会 busy-spin 烧满
			// CPU。短暂退避后重试；ctx 取消则退出。
			select {
			case <-ctx.Done():
				return
			case <-time.After(10 * time.Millisecond):
			}
			continue
		}
		if n == 0 {
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
		ts.parseErr.Add(1)
		if debugPacket {
			log.Printf("packet parse error: %v (len=%d)", err, len(pkt))
		}
		return
	}
	cc, ok := ts.router.Lookup(dst)
	if !ok {
		ts.routeMiss.Add(1)
		if debugPacket {
			log.Printf("no route for %s", dst)
		}
		return
	}
	// Hand off to the per-connection single writer. RecordTx is performed
	// inside writeLoop after the WebSocket frame is actually written, so
	// the server-side Tx count reflects what was sent, not what was queued.
	cc.Enqueue(Packet{Data: pkt})
}

// HandleWebSocket handles incoming WebSocket upgrade requests, authenticates the device,
// registers it in the router, and forwards packets bidirectionally.
func (ts *TunnelServer) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	// 仅接受 Authorization: Bearer <secret> 头。不再支持 ?token= query
	// 参数：query 会被写进访问日志/代理日志/浏览器历史，泄漏设备密钥。
	// 客户端（TunnelClient）始终通过 Authorization 头携带密钥。
	secret := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
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

	cc := NewClientConn(conn, dev.ID, ip, DefaultSendQueueSize)
	ts.router.Register(ip, cc)
	defer func() {
		// 用 match-aware 清理：若期间设备重连、新连接已覆盖该 IP 的路由，
		// 则不误删新路由。
		ts.router.UnregisterConn(ip, cc)
		cc.Close()
	}()

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
	ts.clientReadLoop(ctx, cc)
}

// clientReadLoop reads packets from a WebSocket client and dispatches each
// via routeClientPacket until the context is cancelled or the connection errors.
func (ts *TunnelServer) clientReadLoop(ctx context.Context, cc *ClientConn) {
	for {
		_, pkt, err := cc.Conn.Read(ctx)
		if err != nil {
			return
		}
		cc.RecordRx(len(pkt))
		ts.routeClientPacket(pkt)
	}
}

// routeClientPacket handles a single IP packet arriving from a client:
//   - destination is the server's own VPN IP (full mode only) → inject into TUN;
//   - destination is another registered client → forward over its WS;
//   - no route → routeMiss counter.
//
// In relay mode ts.tun==nil, so the first branch is skipped and every packet
// is either forwarded to another client or counted as a route miss. Packets
// addressed to the legacy server IP (e.g. 10.100.0.1) thus fall through to
// routeMiss — expected, since a relay server offers no in-VPN service.
func (ts *TunnelServer) routeClientPacket(pkt []byte) {
	dst, err := ExtractDstIP(pkt)
	if err != nil {
		ts.parseErr.Add(1)
		if debugPacket {
			log.Printf("packet parse error: %v (len=%d)", err, len(pkt))
		}
		return
	}

	if ts.tun != nil && dst == ts.tunIP {
		buf := make([]byte, meshtun.Offset()+len(pkt))
		copy(buf[meshtun.Offset():], pkt)
		bufs := [][]byte{buf}
		if _, err := ts.tun.Write(bufs, meshtun.Offset()); err != nil {
			log.Printf("write to TUN: %v", err)
		}
		return
	}

	if dest, ok := ts.router.Lookup(dst); ok {
		dest.Enqueue(Packet{Data: pkt})
		return
	}

	ts.routeMiss.Add(1)
	if debugPacket {
		log.Printf("no route for %s", dst)
	}
}

// Close shuts down the TUN device. No-op in relay mode (no TUN was created).
func (ts *TunnelServer) Close() error {
	if ts.tun == nil {
		return nil
	}
	return ts.tun.Close()
}

// Stats returns a snapshot of all connected clients' statistics.
func (ts *TunnelServer) Stats() []ConnStats {
	return ts.router.Stats()
}
