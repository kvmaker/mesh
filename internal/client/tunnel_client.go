package client

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
	meshtun "github.com/maxyu/mesh/internal/common/tun"
)

// TunnelClient connects to the mesh VPN server via WebSocket and shuttles
// IP packets through a local TUN device.
type TunnelClient struct {
	serverURL  string
	secret     string
	mtu        int
	tun        meshtun.Device
	statusDir  string
	httpClient *http.Client

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
//
// tlsConfig 可为 nil（生产环境走系统默认证书校验）；e2e 测试传 InsecureSkipVerify 的配置。
func NewTunnelClient(serverURL, secret, localIP, network string, mtu int, statusDir string, tlsConfig *tls.Config) (*TunnelClient, error) {
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
		// 复用单个 http.Client，避免每次重连新建 Transport 造成空闲连接/fd 泄漏。
		httpClient: &http.Client{Transport: &http.Transport{TLSClientConfig: tlsConfig}},
	}, nil
}

// Run blocks and continuously maintains a connection to the server.
// On connection loss it waits 3 seconds and reconnects. Returns when ctx
// is cancelled.
func (tc *TunnelClient) Run(ctx context.Context) error {
	go tc.writeStatusLoop(ctx)

	// B01: 持久的 TUN 读 goroutine，跨重连复用同一个 TUN fd。tun.Read 在
	// 这里执行并通过 pktCh 投递包，connect 的 main loop 用 select 监听
	// pktCh + ctx.Done，从而能在无背景流量时响应 ctx cancel（SIGTERM/连接
	// 丢失），不再阻塞在 tun.Read 上。
	//
	// 不采用 "ctx cancel 时 close TUN" 方案：close 后 TUN fd 永久失效，Run
	// 的进程内重连会因 tun.Read 立即报错而无限失败。channel 解耦让 TUN fd
	// 生命周期独立于 WS 连接，重连时不需重建 TUN。
	pktCh := make(chan []byte, 128)
	go tc.tunReadLoop(ctx, pktCh)

	for {
		err := tc.connect(ctx, pktCh)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		// connected 由 connect 的 defer 归零，此处无需重复。
		log.Printf("connection lost: %v, reconnecting in 3s...", err)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(3 * time.Second):
		}
	}
}

// tunReadLoop 持续从 TUN 读包并通过 pktCh 投递。在 Run 启动，跨重连复用
// 同一个 TUN fd，直到根 ctx 取消（进程退出）或 TUN 关闭/读出错。
//
// 解耦目的：把会阻塞的 tun.Read 从 connect 的 main loop 移出，使 main loop
// 能用 select 响应 ctx cancel。connect 退出（重连）时本 goroutine 继续运行、
// 继续持有 TUN fd；重连后的新 connect 继续从 pktCh 读，TUN 无需重建。
func (tc *TunnelClient) tunReadLoop(ctx context.Context, pktCh chan<- []byte) {
	bufs := make([][]byte, 1)
	bufs[0] = make([]byte, meshtun.Offset()+tc.mtu+100)
	sizes := make([]int, 1)
	for {
		n, err := tc.tun.Read(bufs, sizes, meshtun.Offset())
		if err != nil {
			// ctx 取消（正常关闭）静默退出；其余为 unexpected error，记录日志后退出。
			if ctx.Err() == nil {
				log.Printf("tun read error: %v", err)
			}
			return
		}
		if n == 0 {
			continue
		}
		pkt := make([]byte, sizes[0])
		copy(pkt, bufs[0][meshtun.Offset():meshtun.Offset()+sizes[0]])
		select {
		case <-ctx.Done():
			return
		case pktCh <- pkt:
		}
	}
}

// connect dials the WebSocket server and runs the bidirectional packet loop
// until either the context is cancelled or an error occurs.
func (tc *TunnelClient) connect(ctx context.Context, pktCh <-chan []byte) error {
	header := http.Header{}
	header.Set("Authorization", "Bearer "+tc.secret)

	conn, _, err := websocket.Dial(ctx, tc.serverURL, &websocket.DialOptions{
		HTTPHeader: header,
		HTTPClient: tc.httpClient,
	})
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	defer conn.CloseNow()

	tc.connected.Store(1)
	// 用 defer 清零：connect 有多条退出路径（ctx 取消、读/写出错、ping
	// 失败），且 Run 在 ctx 取消时会直接 return 而不再执行循环体末尾的
	// Store(0)。放在这里保证任何路径退出后状态都归零，避免关闭/断连后
	// status.json 仍显示 connected=1。
	defer tc.connected.Store(0)
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
				// 给 Ping 独立的超时 ctx：半死连接（对端不回 pong 但
				// TCP 未断）下，共享 ctx 的 Ping 会无限阻塞，探测不到死
				// 连接。5s 超时后 Ping 返回错误，触发 cancel 走重连。
				pingCtx, pingCancel := context.WithTimeout(ctx, 5*time.Second)
				err := conn.Ping(pingCtx)
				pingCancel()
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

	// TUN → WS: main loop reads packets (produced by the persistent
	// tunReadLoop via pktCh) and sends over WebSocket.
	//
	// B01: tun.Read 已移到 tunReadLoop，main loop 只需 select pktCh + ctx.Done，
	// 无背景流量时也能立即响应 ctx cancel（SIGTERM/连接丢失），不再阻塞。
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case pkt := <-pktCh:
			if err := conn.Write(ctx, websocket.MessageBinary, pkt); err != nil {
				return fmt.Errorf("write WS: %w", err)
			}
			tc.txPackets.Add(1)
			tc.txBytes.Add(uint64(len(pkt)))
			tc.lastActive.Store(time.Now().UnixNano())
		}
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
	data, err := json.MarshalIndent(stats, "", "  ")
	if err != nil {
		log.Printf("marshal status: %v", err)
		return
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		log.Printf("write status: %v", err)
		return
	}
	// 原子替换：写临时文件再 rename，避免读者读到半截 JSON。rename 失败
	// 时清理临时文件，防止 .tmp 残留累积。
	if err := os.Rename(tmp, path); err != nil {
		log.Printf("rename status: %v", err)
		os.Remove(tmp)
	}
}

// Close shuts down the TUN device.
func (tc *TunnelClient) Close() error {
	return tc.tun.Close()
}
