package api

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/maxyu/mesh/internal/device"
)

type contextKey string

const contextKeyDeviceID contextKey = "device_id"

// withAuth 验证 Authorization: Bearer <secret> 请求头，找到对应设备后注入 device ID 到 context
func (s *Server) withAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(auth, "Bearer ") {
			http.Error(w, "missing authorization", http.StatusUnauthorized)
			return
		}
		secret := strings.TrimPrefix(auth, "Bearer ")

		// 查找拥有此 secret 的设备
		devices, err := device.List(s.db)
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		var deviceID string
		for _, d := range devices {
			if d.Secret == secret {
				deviceID = d.ID
				break
			}
		}
		if deviceID == "" {
			http.Error(w, "invalid secret", http.StatusUnauthorized)
			return
		}

		ctx := context.WithValue(r.Context(), contextKeyDeviceID, deviceID)
		next(w, r.WithContext(ctx))
	}
}

// rateLimiter 是 per-Server 实例的内存速率限制器
type rateLimiter struct {
	mu       sync.Mutex
	requests map[string][]time.Time
}

func newRateLimiter() *rateLimiter {
	return &rateLimiter{
		requests: make(map[string][]time.Time),
	}
}

// withRateLimit 对注册端点限制每分钟同一 IP 最多 5 次请求
func (s *Server) withRateLimit(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ip := strings.Split(r.RemoteAddr, ":")[0]
		s.limiter.mu.Lock()
		now := time.Now()
		// 清理 1 分钟前的记录
		var recent []time.Time
		for _, t := range s.limiter.requests[ip] {
			if now.Sub(t) < time.Minute {
				recent = append(recent, t)
			}
		}
		if len(recent) >= 5 {
			s.limiter.mu.Unlock()
			http.Error(w, "rate limited", http.StatusTooManyRequests)
			return
		}
		s.limiter.requests[ip] = append(recent, now)
		s.limiter.mu.Unlock()
		next(w, r)
	}
}
