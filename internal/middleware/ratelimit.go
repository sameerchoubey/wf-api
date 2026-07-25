package middleware

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"wealthflow/backend/internal/respond"
)

type rateEntry struct {
	count       int
	windowStart time.Time
}

// RateLimit allows at most limit requests per window per client IP.
// Fixed-window counting is coarse but dependency-free, which is enough
// to blunt credential stuffing and email enumeration on auth routes.
func RateLimit(limit int, window time.Duration) func(http.Handler) http.Handler {
	var (
		mu      sync.Mutex
		entries = make(map[string]*rateEntry)
	)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := clientIP(r)
			now := time.Now()

			mu.Lock()
			// Opportunistic purge so the map can't grow unbounded.
			if len(entries) > 4096 {
				for k, e := range entries {
					if now.Sub(e.windowStart) > window {
						delete(entries, k)
					}
				}
			}
			e, ok := entries[ip]
			if !ok || now.Sub(e.windowStart) > window {
				e = &rateEntry{windowStart: now}
				entries[ip] = e
			}
			e.count++
			exceeded := e.count > limit
			mu.Unlock()

			if exceeded {
				respond.Error(w, http.StatusTooManyRequests, "Too many attempts. Please try again later.")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// clientIP resolves the real client address behind the Fly.io proxy.
func clientIP(r *http.Request) string {
	if ip := r.Header.Get("Fly-Client-IP"); ip != "" {
		return ip
	}
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if first := strings.TrimSpace(strings.Split(xff, ",")[0]); first != "" {
			return first
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
