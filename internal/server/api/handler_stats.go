package api

import (
	"encoding/json"
	"net/http"

	"github.com/maxyu/mesh/internal/server/tunnel"
)

func (s *Server) handleDeviceStats(w http.ResponseWriter, r *http.Request) {
	var stats []tunnel.ConnStats
	if s.tunnel != nil {
		stats = s.tunnel.Stats()
	}
	if stats == nil {
		stats = []tunnel.ConnStats{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stats)
}
