package api

import (
	"context"
	"crypto/tls"
	"database/sql"
	"fmt"
	"net/http"

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

// ListenAndServeTLS starts the HTTPS server using autocert (Let's Encrypt).
// It also starts a :80 listener to serve the HTTP-01 ACME challenge.
func (s *Server) ListenAndServeTLS(ctx context.Context) error {
	m := &autocert.Manager{
		Cache:      autocert.DirCache(s.cfg.CertDir),
		Prompt:     autocert.AcceptTOS,
		HostPolicy: autocert.HostWhitelist(s.cfg.Domain),
	}

	srv := &http.Server{
		Addr:    s.cfg.ListenAddr,
		Handler: s.Handler(),
		TLSConfig: &tls.Config{
			GetCertificate: m.GetCertificate,
			MinVersion:     tls.VersionTLS12,
		},
	}

	// HTTP-01 ACME challenge listener on :80
	go http.ListenAndServe(":80", m.HTTPHandler(nil)) //nolint:errcheck

	go func() {
		<-ctx.Done()
		srv.Close() //nolint:errcheck
	}()

	return srv.ListenAndServeTLS("", "")
}
