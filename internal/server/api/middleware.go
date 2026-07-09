package api

import (
	"net"
	"net/http"
	"sync"
	"time"
)

// rate limit 参数：每个 IP 每 rateWindow 内最多 rateMax 次请求。
const (
	rateWindow = time.Minute
	rateMax    = 5
)

// rateLimiter tracks per-IP request counts within a sliding time window.
type rateLimiter struct {
	mu        sync.Mutex
	requests  map[string][]time.Time
	lastSweep time.Time
}

var limiter = &rateLimiter{requests: make(map[string][]time.Time)}

// resetLimiter 清空全局限流器状态。仅供测试在用例之间隔离状态使用，
// 避免因共享的 package 级 limiter 导致相邻用例互相污染计数。
func resetLimiter() {
	limiter.mu.Lock()
	defer limiter.mu.Unlock()
	limiter.requests = make(map[string][]time.Time)
	limiter.lastSweep = time.Time{}
}

// sweepLocked 删除所有时间戳均已过期的 IP 条目，防止 requests map 随
// 历史访问过的不同 IP 数量无界增长（时间戳会过期但 key 不会自动消失）。
// 调用方必须持有 l.mu。做节流：距上次清理不足一个窗口则跳过，把全表
// 扫描的成本摊薄到 O(1)/请求。
func (l *rateLimiter) sweepLocked(now time.Time) {
	if now.Sub(l.lastSweep) < rateWindow {
		return
	}
	l.lastSweep = now
	for ip, times := range l.requests {
		fresh := false
		for _, t := range times {
			if now.Sub(t) < rateWindow {
				fresh = true
				break
			}
		}
		if !fresh {
			delete(l.requests, ip)
		}
	}
}

// withRateLimit wraps a handler to enforce a rate limit of rateMax requests per rateWindow per IP.
func withRateLimit(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// 用 net.SplitHostPort 正确剥离端口：strings.Split(":") 对 IPv6
		// 地址（如 [::1]:1234）会截断成 "[" 从而把所有 IPv6 客户端归成
		// 同一个 key。SplitHostPort 失败时（无端口）回退用原始 RemoteAddr。
		ip, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			ip = r.RemoteAddr
		}
		limiter.mu.Lock()
		now := time.Now()
		limiter.sweepLocked(now)
		var recent []time.Time
		for _, t := range limiter.requests[ip] {
			if now.Sub(t) < rateWindow {
				recent = append(recent, t)
			}
		}
		if len(recent) >= rateMax {
			// 写回裁剪后的切片，避免保留已过期的时间戳。
			limiter.requests[ip] = recent
			limiter.mu.Unlock()
			http.Error(w, "rate limited", http.StatusTooManyRequests)
			return
		}
		limiter.requests[ip] = append(recent, now)
		limiter.mu.Unlock()
		next(w, r)
	}
}
