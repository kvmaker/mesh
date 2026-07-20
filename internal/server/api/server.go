package api

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"database/sql"
	"fmt"
	"log"
	"math/big"
	"net"
	"net/http"
	"strings"
	"time"

	"golang.org/x/crypto/acme/autocert"

	"github.com/maxyu/mesh/internal/server/config"
	"github.com/maxyu/mesh/internal/server/device"
	"github.com/maxyu/mesh/internal/server/token"
	"github.com/maxyu/mesh/internal/server/tunnel"
)

// Server is the HTTP API server for the mesh VPN control plane.
type Server struct {
	db     *sql.DB
	cfg    *config.Config
	tunnel *tunnel.TunnelServer
}

// New creates a new API Server. ts may be nil (e.g. in tests).
func New(db *sql.DB, cfg *config.Config, ts *tunnel.TunnelServer) *Server {
	return &Server{db: db, cfg: cfg, tunnel: ts}
}

// Handler returns the HTTP handler for all API routes.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", s.handleCoverPage)
	mux.HandleFunc("POST /api/devices/register", withRateLimit(s.handleRegister))
	mux.HandleFunc("GET /api/devices", s.withAuth(s.handleDeviceList))
	mux.HandleFunc("GET /api/stats/devices", s.withAuth(s.handleDeviceStats))
	mux.HandleFunc("GET /tunnel", s.handleTunnel)
	return mux
}

// withAuth 保护需要凭证的读接口。接受 Authorization: Bearer <secret>，
// 其中 secret 可以是服务端管理 token（CLI 拉取 stats 用），也可以是任一
// 已注册设备的密钥（client `mesh peers` 用）。两类合法调用方都持有其中
// 一种凭证，未携带或无效凭证一律拒绝，避免设备列表/IP 裸奔泄漏。
func (s *Server) withAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		secret := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if secret == "" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if token.Verify(s.db, secret) {
			next(w, r)
			return
		}
		if _, err := device.GetBySecret(s.db, secret); err == nil {
			next(w, r)
			return
		}
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}
}

// handleCoverPage serves a minimal cover page to disguise the service.
func (s *Server) handleCoverPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	fmt.Fprint(w, `<!DOCTYPE html><html><head><title>Welcome</title></head><body><h1>It works!</h1><p>Service is running.</p></body></html>`)
}

// handleTunnel delegates to the tunnel WebSocket handler, guarding against a nil tunnel.
func (s *Server) handleTunnel(w http.ResponseWriter, r *http.Request) {
	if s.tunnel == nil {
		http.Error(w, "tunnel not available", http.StatusServiceUnavailable)
		return
	}
	s.tunnel.HandleWebSocket(w, r)
}

// serveMode 决定 HTTPS 服务如何启动:
//   - "selfsigned": TLSTestMode(e2e 测试,内存自签证书)
//   - "plain":      tls_mode: none(relay + 反向代理,纯 HTTP,不启动 autocert)
//   - "autocert":   默认(Let's Encrypt + :80 ACME challenge)
//
// TLSTestMode(env MESH_TEST_TLS)优先级高于 yaml tls_mode:测试模式恒走自签。
func (s *Server) serveMode() string {
	if s.cfg.TLSTestMode {
		return "selfsigned"
	}
	if s.cfg.TLSMode == config.TLSNone {
		return "plain"
	}
	return "autocert"
}

// ListenAndServeTLS starts the HTTPS server.
//
// 生产环境使用 autocert（Let's Encrypt）+ :80 ACME challenge listener。
// 当 cfg.TLSTestMode 为真（即 e2e 测试设置了 MESH_TEST_TLS=on）时，
// 改用内存自签证书，完全不依赖 Let's Encrypt，也不监听 :80。
// 当 tls_mode: none 时（relay 模式 + 反向代理），走纯 HTTP，
// 既不启动 autocert 也不监听 :80。
func (s *Server) ListenAndServeTLS(ctx context.Context) error {
	srv := &http.Server{
		Addr:    s.cfg.ListenAddr,
		Handler: s.Handler(),
		// 防慢连接攻击（Slowloris）：限制读取请求头的最长时长。
		// 注意：这里不能设置 ReadTimeout/WriteTimeout，因为 /tunnel 是
		// 长连接 WebSocket，整体读写超时会在 upgrade 后强制断开隧道。
		// ReadHeaderTimeout 只约束 handler 执行前的 header 读取阶段，
		// 不影响 upgrade 之后的双向数据流。
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		<-ctx.Done()
		srv.Close() //nolint:errcheck
	}()

	switch s.serveMode() {
	case "selfsigned":
		tlsCfg, err := s.selfSignedTLSConfig()
		if err != nil {
			return fmt.Errorf("self-signed cert: %w", err)
		}
		srv.TLSConfig = tlsCfg
		return srv.ListenAndServeTLS("", "")
	case "plain":
		// tls_mode: none — 纯 HTTP,适用于 relay 模式配合反向代理(Caddy)。
		// 不启动 autocert,不监听 :80。
		return srv.ListenAndServe()
	default: // "autocert"
		m := &autocert.Manager{
			Cache:      autocert.DirCache(s.cfg.CertDir),
			Prompt:     autocert.AcceptTOS,
			HostPolicy: autocert.HostWhitelist(s.cfg.Domain),
		}
		srv.TLSConfig = &tls.Config{
			GetCertificate: m.GetCertificate,
			MinVersion:     tls.VersionTLS12,
		}
		// HTTP-01 ACME challenge listener on :80。监听失败（如 :80 被占用、
		// 权限不足）会导致 Let's Encrypt 签发失败，必须记录而不是静默吞掉。
		go func() {
			if err := http.ListenAndServe(":80", m.HTTPHandler(nil)); err != nil {
				log.Printf("ACME HTTP-01 listener on :80 failed: %v", err)
			}
		}()
		return srv.ListenAndServeTLS("", "")
	}
}

// selfSignedTLSConfig 生成一份内存中的 ECDSA P-256 自签证书，
// 仅供 e2e 测试使用。NotAfter 24h，IsCA=true，
// DNSNames 含 domain/localhost/server，IPAddresses 含 127.0.0.1。
func (s *Server) selfSignedTLSConfig() (*tls.Config, error) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate key: %w", err)
	}

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, fmt.Errorf("serial: %w", err)
	}

	now := time.Now()
	tmpl := x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName: s.cfg.Domain,
		},
		NotBefore:             now.Add(-1 * time.Hour),
		NotAfter:              now.Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
		DNSNames:              []string{s.cfg.Domain, "localhost", "server"},
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
	}

	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &priv.PublicKey, priv)
	if err != nil {
		return nil, fmt.Errorf("create cert: %w", err)
	}

	return &tls.Config{
		Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: priv}},
		MinVersion:   tls.VersionTLS12,
	}, nil
}
