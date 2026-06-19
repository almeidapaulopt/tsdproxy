// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package proxymanager

import (
	"container/list"
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
	httpRateLimitClients = 4096
)

type rateLimitEntry struct {
	lastSeen time.Time
	limiter  *rate.Limiter
	elem     *list.Element
	key      string
}

type ipRateLimiter struct {
	clients map[string]*rateLimitEntry
	lruList *list.List
	rate    rate.Limit
	burst   int
	mu      sync.Mutex
}

func newIPRateLimiter(r rate.Limit, b int) *ipRateLimiter {
	return &ipRateLimiter{
		clients: make(map[string]*rateLimitEntry),
		lruList: list.New(),
		rate:    r,
		burst:   b,
	}
}

func (l *ipRateLimiter) get(clientIP string) *rate.Limiter {
	l.mu.Lock()
	defer l.mu.Unlock()

	if entry, ok := l.clients[clientIP]; ok {
		entry.lastSeen = time.Now()
		l.lruList.MoveToFront(entry.elem)
		return entry.limiter
	}

	if len(l.clients) >= httpRateLimitClients {
		if oldest := l.lruList.Back(); oldest != nil {
			oldestEntry := oldest.Value.(*rateLimitEntry)
			delete(l.clients, oldestEntry.key)
			l.lruList.Remove(oldest)
		}
	}

	entry := &rateLimitEntry{
		key:      clientIP,
		limiter:  rate.NewLimiter(l.rate, l.burst),
		lastSeen: time.Now(),
	}
	entry.elem = l.lruList.PushFront(entry)
	l.clients[clientIP] = entry

	return entry.limiter
}

func (l *ipRateLimiter) allow(clientIP string) bool {
	return l.get(clientIP).Allow()
}

func (l *ipRateLimiter) close() {
	l.mu.Lock()
	l.clients = make(map[string]*rateLimitEntry)
	l.lruList.Init()
	l.mu.Unlock()
}

func rateLimitMiddleware(limiter *ipRateLimiter, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := resolvePeerIP(r)
		if ip == "" {
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

func resolvePeerIP(r *http.Request) string {
	if !model.IsLocalhost(r.RemoteAddr) {
		return model.NormalizeIP(r.RemoteAddr)
	}

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
