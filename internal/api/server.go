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
	"math/big"
	"net"
	"net/http"
	"time"

	"golang.org/x/crypto/acme/autocert"

	"github.com/maxyu/mesh/internal/config"
	"github.com/maxyu/mesh/internal/tunnel"
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
	mux.HandleFunc("GET /api/devices", s.handleDeviceList)
	mux.HandleFunc("GET /api/stats/devices", s.handleDeviceStats)
	mux.HandleFunc("GET /tunnel", s.handleTunnel)
	return mux
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

// ListenAndServeTLS starts the HTTPS server.
//
// 生产环境使用 autocert（Let's Encrypt）+ :80 ACME challenge listener。
// 当 cfg.TLSTestMode 为真（即 e2e 测试设置了 MESH_TEST_TLS=off）时，
// 改用内存自签证书，完全不依赖 Let's Encrypt，也不监听 :80。
func (s *Server) ListenAndServeTLS(ctx context.Context) error {
	srv := &http.Server{
		Addr:    s.cfg.ListenAddr,
		Handler: s.Handler(),
	}

	go func() {
		<-ctx.Done()
		srv.Close() //nolint:errcheck
	}()

	if s.cfg.TLSTestMode {
		tlsCfg, err := s.selfSignedTLSConfig()
		if err != nil {
			return fmt.Errorf("self-signed cert: %w", err)
		}
		srv.TLSConfig = tlsCfg
		return srv.ListenAndServeTLS("", "")
	}

	// 生产路径：autocert + Let's Encrypt
	m := &autocert.Manager{
		Cache:      autocert.DirCache(s.cfg.CertDir),
		Prompt:     autocert.AcceptTOS,
		HostPolicy: autocert.HostWhitelist(s.cfg.Domain),
	}
	srv.TLSConfig = &tls.Config{
		GetCertificate: m.GetCertificate,
		MinVersion:     tls.VersionTLS12,
	}

	// HTTP-01 ACME challenge listener on :80
	go http.ListenAndServe(":80", m.HTTPHandler(nil)) //nolint:errcheck

	return srv.ListenAndServeTLS("", "")
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
