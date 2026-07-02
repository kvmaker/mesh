package api

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"

	"github.com/google/uuid"
	"github.com/maxyu/mesh/internal/device"
	"github.com/maxyu/mesh/internal/token"
	"github.com/maxyu/mesh/internal/wg"
)

func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	var req RegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if !token.Verify(s.db, req.Token) {
		http.Error(w, "invalid token", http.StatusUnauthorized)
		return
	}

	// 幂等：如果公钥已注册，返回已有信息
	existing, err := device.GetByPublicKey(s.db, req.PublicKey)
	if err == nil {
		serverPubKey := s.loadServerPublicKey()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(RegisterResponse{
			AssignedIP:      existing.IP,
			ServerPublicKey: serverPubKey,
			ServerEndpoint:  s.cfg.Endpoint,
			NetworkCIDR:     s.cfg.Network,
			DeviceSecret:    existing.Secret,
			DeviceID:        existing.ID,
		})
		return
	}

	// 分配 IP（Allocate 内部会插入临时占位记录）
	ip, err := device.Allocate(s.db, s.cfg.Network)
	if err != nil {
		http.Error(w, "no available IPs", http.StatusServiceUnavailable)
		return
	}

	// 生成 device secret
	secretBytes := make([]byte, 32)
	if _, err := rand.Read(secretBytes); err != nil {
		http.Error(w, "failed to generate secret", http.StatusInternalServerError)
		return
	}
	secret := hex.EncodeToString(secretBytes)

	// 删除临时占位记录，然后创建真正的设备记录
	// Allocate 函数已插入临时 record，需要先 Release 再 Create 正式 record
	// 但 Release 按 IP 删除，会删掉临时记录，然后再插入正式记录（包含相同 IP）
	device.Release(s.db, ip)

	d := &device.Device{
		ID:        uuid.New().String(),
		Name:      req.Hostname,
		PublicKey: req.PublicKey,
		IP:        ip,
		Secret:    secret,
		Passive:   false,
	}
	if err := device.Create(s.db, d); err != nil {
		http.Error(w, "failed to create device", http.StatusInternalServerError)
		return
	}

	// 添加 WireGuard peer（测试中跳过）
	if s.wgIface != "wg0-test" {
		wg.AddPeer(s.wgIface, req.PublicKey, ip)
	}

	serverPubKey := s.loadServerPublicKey()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(RegisterResponse{
		AssignedIP:      ip,
		ServerPublicKey: serverPubKey,
		ServerEndpoint:  s.cfg.Endpoint,
		NetworkCIDR:     s.cfg.Network,
		DeviceSecret:    secret,
		DeviceID:        d.ID,
	})
}

// loadServerPublicKey 从 DataDir 读取服务器公钥；文件不存在时返回空字符串
func (s *Server) loadServerPublicKey() string {
	keyPath := filepath.Join(s.cfg.DataDir, "server.pub")
	data, err := os.ReadFile(keyPath)
	if err != nil {
		return ""
	}
	return string(data)
}
