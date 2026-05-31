// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package tailscale

import (
	"net"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"

	"github.com/almeidapaulopt/tsdproxy/internal/model"
)

type whoisCacheEntry struct {
	expiresAt time.Time
	who       model.Whois
}

// WhoisCache is a thread-safe, two-layer cache for model.Whois results.
// Layer 1: TTL-based map for sub-millisecond cache hits.
// Layer 2: singleflight.Group to deduplicate concurrent WhoIs RPCs on cache miss.
type WhoisCache struct {
	sf         singleflight.Group
	entries    map[string]*whoisCacheEntry
	ttl        time.Duration
	maxEntries int
	mu         sync.RWMutex
}

func NewWhoisCache(ttl time.Duration, maxEntries ...int) *WhoisCache {
	maxCount := 0
	if len(maxEntries) > 0 {
		maxCount = maxEntries[0]
	}
	return &WhoisCache{
		entries:    make(map[string]*whoisCacheEntry),
		ttl:        ttl,
		maxEntries: maxCount,
	}
}

// Lookup resolves the Whois identity for ip. On cache hit the result is
// returned immediately. On cache miss, singleflight ensures that only one
// goroutine calls resolve — all others wait and share the result. The
// fresh result is stored in the TTL cache before being returned.
func (c *WhoisCache) Lookup(ip string, resolve func() (model.Whois, error)) (model.Whois, error) {
	key := NormalizeIP(ip)
	if key == "" {
		return model.Whois{}, nil
	}

	c.mu.RLock()
	if entry, ok := c.entries[key]; ok && time.Now().Before(entry.expiresAt) {
		c.mu.RUnlock()
		return entry.who, nil
	}
	c.mu.RUnlock()

	v, err, _ := c.sf.Do(key, func() (interface{}, error) {
		who, resolveErr := resolve()
		if resolveErr != nil {
			return nil, resolveErr
		}
		if who.ID != "" {
			c.mu.Lock()
			c.entries[key] = &whoisCacheEntry{who: who, expiresAt: time.Now().Add(c.ttl)}
			if c.maxEntries > 0 && len(c.entries) > c.maxEntries {
				c.evict()
			}
			c.mu.Unlock()
		}
		return who, nil
	})
	if err != nil {
		return model.Whois{}, err
	}
	return v.(model.Whois), nil
}

func (c *WhoisCache) evict() {
	now := time.Now()
	for k, e := range c.entries {
		if now.After(e.expiresAt) {
			delete(c.entries, k)
		}
	}
	if c.maxEntries <= 0 || len(c.entries) <= c.maxEntries {
		return
	}
	for len(c.entries) > c.maxEntries {
		var oldest string
		oldestTime := time.Now().Add(c.ttl + time.Hour)
		for k, e := range c.entries {
			if e.expiresAt.Before(oldestTime) {
				oldestTime = e.expiresAt
				oldest = k
			}
		}
		delete(c.entries, oldest)
	}
}

// NormalizeIP takes an address in "ip:port" or bare IP form and returns
// the normalized IP string. It trims whitespace and strips any port
// component. Returns empty string if the input cannot be parsed.
func NormalizeIP(addr string) string {
	addr = trim(addr)

	// Try splitting host:port first (handles "1.2.3.4:443" and "[::1]:443").
	host, _, err := net.SplitHostPort(addr)
	if err == nil {
		if ip := net.ParseIP(host); ip != nil {
			return host
		}
		return ""
	}

	// No port present — validate as a bare IP.
	if ip := net.ParseIP(addr); ip != nil {
		return addr
	}

	return ""
}

func trim(s string) string {
	for len(s) > 0 && (s[0] == ' ' || s[0] == '\t') {
		s = s[1:]
	}
	for len(s) > 0 && (s[len(s)-1] == ' ' || s[len(s)-1] == '\t') {
		s = s[:len(s)-1]
	}
	return s
}
