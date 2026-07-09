package api

import (
	"encoding/json"
	"net/http"

	"github.com/google/uuid"
	"github.com/maxyu/mesh/internal/server/device"
	"github.com/maxyu/mesh/internal/server/token"
)

// RegisterRequest is the JSON body for POST /api/devices/register.
type RegisterRequest struct {
	Token    string `json:"token"`
	Hostname string `json:"hostname"`
}

// RegisterResponse is the JSON response for a successful device registration.
type RegisterResponse struct {
	AssignedIP   string `json:"assigned_ip"`
	DeviceSecret string `json:"device_secret"`
	DeviceID     string `json:"device_id"`
	NetworkCIDR  string `json:"network_cidr"`
}

// handleRegister registers a new device in the mesh network.
func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	// 在 token 校验前就限制请求体大小，防止未认证客户端用超大 body 耗尽内存。
	// 4KB 对该请求（仅 token + hostname）绰绰有余。
	r.Body = http.MaxBytesReader(w, r.Body, 4<<10)
	var req RegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}

	if !token.Verify(s.db, req.Token) {
		http.Error(w, "invalid token", http.StatusUnauthorized)
		return
	}

	secret, err := token.Generate()
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	dev, err := device.AllocateAndCreate(s.db, s.cfg.Network, uuid.New().String(), req.Hostname, secret)
	if err != nil {
		http.Error(w, "no available IPs", http.StatusServiceUnavailable)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(RegisterResponse{
		AssignedIP:   dev.IP,
		DeviceSecret: dev.Secret,
		DeviceID:     dev.ID,
		NetworkCIDR:  s.cfg.Network,
	})
}
