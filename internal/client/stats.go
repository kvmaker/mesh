package client

import "time"

// ClientStats holds runtime statistics written by the tunnel client process
// to ~/.mesh/status.json for the `mesh status` command to read.
type ClientStats struct {
	Connected   bool      `json:"connected"`
	ConnectedAt time.Time `json:"connected_at,omitzero"`
	TxPackets   uint64    `json:"tx_packets"`
	TxBytes     uint64    `json:"tx_bytes"`
	RxPackets   uint64    `json:"rx_packets"`
	RxBytes     uint64    `json:"rx_bytes"`
	LastRTT     int64     `json:"rtt_us"`
	LastActive  time.Time `json:"last_active,omitzero"`
	PID         int       `json:"pid"`
	UpdatedAt   time.Time `json:"updated_at"`
}
