// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package proxymanager

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"

	"github.com/almeidapaulopt/tsdproxy/internal/consts"
	"github.com/almeidapaulopt/tsdproxy/internal/model"
)

const (
	httpRateLimitClients = 4096 // max tracked IPs
)

type rateLimitEntry struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

type ipRateLimiter struct {
	clients map[string]*rateLimitEntry
	rate    rate.Limit
	burst   int
	mu      sync.Mutex
}

func newIPRateLimiter(r rate.Limit, b int) *ipRateLimiter {
	return &ipRateLimiter{
		clients: make(map[string]*rateLimitEntry),
		rate:    r,
		burst:   b,
	}
}

func (l *ipRateLimiter) get(clientIP string) *rate.Limiter {
	l.mu.Lock()
	defer l.mu.Unlock()
	if entry, ok := l.clients[clientIP]; ok {
		entry.lastSeen = time.Now()
		return entry.limiter
	}
	if len(l.clients) >= httpRateLimitClients {
		// Evict the oldest entry (deterministic LRU-like strategy).
		var oldestKey string
		var oldestTime time.Time
		for k, v := range l.clients {
			if oldestKey == "" || v.lastSeen.Before(oldestTime) {
				oldestKey = k
				oldestTime = v.lastSeen
			}
		}
		delete(l.clients, oldestKey)
	}
	entry := &rateLimitEntry{
		limiter:  rate.NewLimiter(l.rate, l.burst),
		lastSeen: time.Now(),
	}
	l.clients[clientIP] = entry
	return entry.limiter
}

func (l *ipRateLimiter) allow(clientIP string) bool {
	return l.get(clientIP).Allow()
}

func (l *ipRateLimiter) close() {
	l.mu.Lock()
	l.clients = make(map[string]*rateLimitEntry)
	l.mu.Unlock()
}

func rateLimitMiddleware(limiter *ipRateLimiter, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := resolvePeerIP(r)
		if ip == "" {
			// Fall back to RemoteAddr so requests with an unresolvable peer
			// IP (e.g. services/VIP mode with missing/spoofed XFF) are still
			// rate-limited instead of silently exempted.
			ip = r.RemoteAddr
		}
		if !limiter.allow(ip) {
			w.Header().Set("Retry-After", "1")
			http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// resolvePeerIP extracts the single authoritative client IP from a request.
//
// When RemoteAddr is localhost (services/VIP proxy hop), the original
// client IP is read from the inbound X-Forwarded-For with anti-spoofing:
//   - exactly one X-Forwarded-For header must be present
//   - the value must be a single IP (no comma-separated chain)
//   - loopback addresses are rejected
//
// For non-localhost connections RemoteAddr is used directly.
func resolvePeerIP(r *http.Request) string {
	if !model.IsLocalhost(r.RemoteAddr) {
		return model.NormalizeIP(r.RemoteAddr)
	}

	// Trusted proxy hop: extract from X-Forwarded-For.
	xffVals := r.Header.Values(consts.HeaderXForwardedFor)
	if len(xffVals) != 1 {
		return ""
	}
	if strings.Contains(xffVals[0], ",") {
		return ""
	}
	ip := model.NormalizeIP(xffVals[0])
	if parsed := net.ParseIP(ip); parsed != nil && parsed.IsLoopback() {
		return ""
	}
	return ip
}
