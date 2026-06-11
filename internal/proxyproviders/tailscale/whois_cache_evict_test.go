// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package tailscale

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/almeidapaulopt/tsdproxy/internal/model"
)

func TestWhoisCache_Eviction_PastExpiryRemoved(t *testing.T) {
	t.Parallel()

	// Use a 1-hour TTL so entries don't expire naturally during the test.
	// maxEntries=2 means the 3rd insert triggers eviction.
	c := NewWhoisCache(time.Hour, 2)
	resolveCount := int32(0)

	who1 := model.Whois{ID: "user-1", DisplayName: "Alice"}
	who2 := model.Whois{ID: "user-2", DisplayName: "Bob"}
	who3 := model.Whois{ID: "user-3", DisplayName: "Charlie"}

	for _, entry := range []struct {
		ip  string
		who model.Whois
	}{
		{"100.64.0.1", who1},
		{"100.64.0.2", who2},
		{"100.64.0.3", who3},
	} {
		result, err := c.Lookup(entry.ip, func() (model.Whois, error) {
			atomic.AddInt32(&resolveCount, 1)
			return entry.who, nil
		})
		require.NoError(t, err)
		require.Equal(t, entry.who, result)
	}

	resolveBefore := atomic.LoadInt32(&resolveCount)
	result, err := c.Lookup("100.64.0.1", func() (model.Whois, error) {
		atomic.AddInt32(&resolveCount, 1)
		return model.Whois{ID: "user-1-relooked", DisplayName: "Alice-v2"}, nil
	})
	require.NoError(t, err)
	require.Equal(t, "user-1-relooked", result.ID)
	require.Equal(t, resolveBefore+1, atomic.LoadInt32(&resolveCount),
		"expected IP 100.64.0.1 to be evicted and re-resolved")
}

func TestWhoisCache_Eviction_ExpiredOnly(t *testing.T) {
	t.Parallel()

	// Very short TTL + maxEntries=5: entries expire before the 5th insert.
	// evict() removes expired entries first, so no hard eviction needed.
	c := NewWhoisCache(10*time.Millisecond, 5)
	resolveCount := int32(0)

	for i := range 5 {
		ip := "100.64.0." + string(rune('0'+i))
		_, err := c.Lookup(ip, func() (model.Whois, error) {
			atomic.AddInt32(&resolveCount, 1)
			return model.Whois{ID: "user", DisplayName: "test"}, nil
		})
		require.NoError(t, err)
	}

	// Wait for all entries to expire.
	time.Sleep(50 * time.Millisecond)

	_, err := c.Lookup("100.64.0.9", func() (model.Whois, error) {
		atomic.AddInt32(&resolveCount, 1)
		return model.Whois{ID: "user-new", DisplayName: "new"}, nil
	})
	require.NoError(t, err)

	resolveBefore := atomic.LoadInt32(&resolveCount)
	_, err = c.Lookup("100.64.0.0", func() (model.Whois, error) {
		atomic.AddInt32(&resolveCount, 1)
		return model.Whois{ID: "user-re-resolved", DisplayName: "after-evict"}, nil
	})
	require.NoError(t, err)
	require.Equal(t, resolveBefore+1, atomic.LoadInt32(&resolveCount),
		"expected expired entry to be re-resolved")
}

func TestWhoisCache_NoEviction_BelowMaxEntries(t *testing.T) {
	t.Parallel()

	c := NewWhoisCache(time.Hour, 10)
	resolveCount := int32(0)

	who1 := model.Whois{ID: "user-1"}
	who2 := model.Whois{ID: "user-2"}

	for _, entry := range []struct {
		ip  string
		who model.Whois
	}{
		{"100.64.0.1", who1},
		{"100.64.0.2", who2},
	} {
		_, err := c.Lookup(entry.ip, func() (model.Whois, error) {
			atomic.AddInt32(&resolveCount, 1)
			return entry.who, nil
		})
		require.NoError(t, err)
	}

	resolveBefore := atomic.LoadInt32(&resolveCount)
	result, err := c.Lookup("100.64.0.1", func() (model.Whois, error) {
		atomic.AddInt32(&resolveCount, 1)
		return model.Whois{}, nil
	})
	require.NoError(t, err)
	require.Equal(t, who1, result)
	require.Equal(t, resolveBefore, atomic.LoadInt32(&resolveCount),
		"expected no re-resolution below maxEntries")
}

func TestWhoisCache_EmptyMaxEntries_NoEviction(t *testing.T) {
	t.Parallel()

	c := NewWhoisCache(time.Hour) // no maxEntries (default 0 = unlimited)
	resolveCount := int32(0)

	for i := range 10 {
		ip := "100.64.0." + string(rune('0'+i))
		_, err := c.Lookup(ip, func() (model.Whois, error) {
			atomic.AddInt32(&resolveCount, 1)
			return model.Whois{ID: "user", DisplayName: "test"}, nil
		})
		require.NoError(t, err)
	}

	resolveBefore := atomic.LoadInt32(&resolveCount)
	result, err := c.Lookup("100.64.0.0", func() (model.Whois, error) {
		atomic.AddInt32(&resolveCount, 1)
		return model.Whois{}, nil
	})
	require.NoError(t, err)
	require.Equal(t, "user", result.ID)
	require.Equal(t, resolveBefore, atomic.LoadInt32(&resolveCount),
		"expected no re-resolution with unlimited entries")
}
