package api

import (
	"net/http"

	"github.com/maxyu/mesh/internal/device"
	"github.com/maxyu/mesh/internal/wg"
)

func (s *Server) handleHeartbeat(w http.ResponseWriter, r *http.Request) {
	deviceID := r.Context().Value(contextKeyDeviceID).(string)
	if err := device.UpdateHeartbeat(s.db, deviceID); err != nil {
		http.Error(w, "heartbeat failed", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (s *Server) handleDelete(w http.ResponseWriter, r *http.Request) {
	deviceID := r.Context().Value(contextKeyDeviceID).(string)
	pathID := r.PathValue("id")

	if deviceID != pathID {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	d, err := device.GetByID(s.db, deviceID)
	if err != nil {
		http.Error(w, "device not found", http.StatusNotFound)
		return
	}

	if s.wgIface != "wg0-test" {
		wg.RemovePeer(s.wgIface, d.PublicKey)
	}

	device.Delete(s.db, deviceID)
	w.WriteHeader(http.StatusOK)
}
