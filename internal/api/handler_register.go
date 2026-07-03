package api

import (
	"encoding/json"
	"net/http"

	"github.com/google/uuid"
	"github.com/maxyu/mesh/internal/device"
	"github.com/maxyu/mesh/internal/token"
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
	var req RegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}

	if !token.Verify(s.db, req.Token) {
		http.Error(w, "invalid token", http.StatusUnauthorized)
		return
	}

	ip, err := device.Allocate(s.db, s.cfg.Network)
	if err != nil {
		http.Error(w, "no available IPs", http.StatusServiceUnavailable)
		return
	}

	secret, err := token.Generate()
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	dev := &device.Device{
		ID:     uuid.New().String(),
		Name:   req.Hostname,
		IP:     ip,
		Secret: secret,
	}
	if err := device.Create(s.db, dev); err != nil {
		http.Error(w, "create failed", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(RegisterResponse{
		AssignedIP:   ip,
		DeviceSecret: secret,
		DeviceID:     dev.ID,
		NetworkCIDR:  s.cfg.Network,
	})
}
