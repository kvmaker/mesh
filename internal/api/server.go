package api

import (
	"database/sql"
	"fmt"
	"net/http"

	"github.com/maxyu/mesh/internal/config"
)

// Server 是 REST API 服务器结构体
type Server struct {
	db      *sql.DB
	cfg     *config.Config
	wgIface string
	limiter *rateLimiter
}

// RegisterRequest 是注册请求的请求体
type RegisterRequest struct {
	Token     string `json:"token"`
	PublicKey string `json:"public_key"`
	Hostname  string `json:"hostname"`
}

// RegisterResponse 是注册请求的响应体
type RegisterResponse struct {
	AssignedIP      string `json:"assigned_ip"`
	ServerPublicKey string `json:"server_public_key"`
	ServerEndpoint  string `json:"server_endpoint"`
	NetworkCIDR     string `json:"network_cidr"`
	DeviceSecret    string `json:"device_secret"`
}

// New 创建一个新的 Server 实例
func New(db *sql.DB, cfg *config.Config, wgIface string) *Server {
	return &Server{
		db:      db,
		cfg:     cfg,
		wgIface: wgIface,
		limiter: newRateLimiter(),
	}
}

// handler 返回 HTTP handler（小写，包内使用）
func (s *Server) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/devices/register", s.withRateLimit(s.handleRegister))
	mux.HandleFunc("POST /api/devices/heartbeat", s.withAuth(s.handleHeartbeat))
	mux.HandleFunc("DELETE /api/devices/{id}", s.withAuth(s.handleDelete))
	return mux
}

// Handler 返回 HTTP handler（公开，供集成测试使用）
func (s *Server) Handler() http.Handler {
	return s.handler()
}

// ListenAndServe 启动 HTTP 服务器
func (s *Server) ListenAndServe() error {
	addr := fmt.Sprintf(":%d", s.cfg.APIPort)
	return http.ListenAndServe(addr, s.handler())
}
