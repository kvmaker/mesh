package api

import (
	"net/http"
	"strings"
	"sync"
	"time"
)

// rateLimiter tracks per-IP request counts within a sliding time window.
type rateLimiter struct {
	mu       sync.Mutex
	requests map[string][]time.Time
}

var limiter = &rateLimiter{requests: make(map[string][]time.Time)}

// withRateLimit wraps a handler to enforce a rate limit of 5 requests per minute per IP.
func withRateLimit(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ip := strings.Split(r.RemoteAddr, ":")[0]
		limiter.mu.Lock()
		now := time.Now()
		var recent []time.Time
		for _, t := range limiter.requests[ip] {
			if now.Sub(t) < time.Minute {
				recent = append(recent, t)
			}
		}
		if len(recent) >= 5 {
			limiter.mu.Unlock()
			http.Error(w, "rate limited", http.StatusTooManyRequests)
			return
		}
		limiter.requests[ip] = append(recent, now)
		limiter.mu.Unlock()
		next(w, r)
	}
}
