package api

import (
	"encoding/json"
	"net/http"

	"github.com/maxyu/mesh/internal/device"
)

type deviceInfo struct {
	Name   string `json:"name"`
	IP     string `json:"ip"`
	Online bool   `json:"online"`
}

func (s *Server) handleDeviceList(w http.ResponseWriter, r *http.Request) {
	devs, err := device.List(s.db)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	list := make([]deviceInfo, len(devs))
	for i, d := range devs {
		list[i] = deviceInfo{Name: d.Name, IP: d.IP, Online: d.Online}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(list)
}
