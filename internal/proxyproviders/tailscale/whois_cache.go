// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package tailscale

import (
	"sync"
	"time"

	"golang.org/x/sync/singleflight"

	"github.com/almeidapaulopt/tsdproxy/internal/model"
)

const whoisNegativeTTL = 10 * time.Second

type whoisCacheEntry struct {
	expiresAt time.Time
	who       model.Whois
	negative  bool
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

func (c *WhoisCache) storeEntry(key string, entry *whoisCacheEntry) {
	c.mu.Lock()
	c.entries[key] = entry
	if c.maxEntries > 0 && len(c.entries) > c.maxEntries {
		c.evict()
	}
	c.mu.Unlock()
}

// Lookup resolves the Whois identity for ip. On cache hit the result is
// returned immediately. On cache miss, singleflight ensures that only one
// goroutine calls resolve — all others wait and share the result. The
// fresh result is stored in the TTL cache before being returned.
func (c *WhoisCache) Lookup(ip string, resolve func() (model.Whois, error)) (model.Whois, error) {
	key := model.NormalizeIP(ip)
	if key == "" {
		return model.Whois{}, nil
	}

	c.mu.RLock()
	if entry, ok := c.entries[key]; ok && time.Now().Before(entry.expiresAt) {
		c.mu.RUnlock()
		if entry.negative {
			return model.Whois{}, nil
		}
		return entry.who, nil
	}
	c.mu.RUnlock()

	v, err, _ := c.sf.Do(key, func() (any, error) {
		who, resolveErr := resolve()
		if resolveErr != nil {
			return nil, resolveErr
		}
		ttl := c.ttl
		if who.ID == "" {
			ttl = whoisNegativeTTL
		}
		c.storeEntry(key, &whoisCacheEntry{
			who:       who,
			expiresAt: time.Now().Add(ttl),
			negative:  who.ID == "",
		})
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
