// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package tailscale

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/almeidapaulopt/tsdproxy/internal/model"
)

func TestWhoisCache_CacheHit(t *testing.T) {
	t.Parallel()

	c := NewWhoisCache(30 * time.Second)
	resolveCount := int32(0)

	who := model.Whois{ID: "user-1", DisplayName: "Alice"}

	result, err := c.Lookup("100.64.0.1", func() (model.Whois, error) {
		atomic.AddInt32(&resolveCount, 1)
		return who, nil
	})
	require.NoError(t, err)
	assert.Equal(t, who, result)

	result2, err2 := c.Lookup("100.64.0.1", func() (model.Whois, error) {
		atomic.AddInt32(&resolveCount, 1)
		return model.Whois{}, nil
	})
	require.NoError(t, err2)
	assert.Equal(t, who, result2)

	assert.Equal(t, int32(1), atomic.LoadInt32(&resolveCount))
}

func TestWhoisCache_TTLExpiry(t *testing.T) {
	t.Parallel()

	c := NewWhoisCache(50 * time.Millisecond)
	resolveCount := int32(0)

	who := model.Whois{ID: "user-1", DisplayName: "Alice"}

	result, err := c.Lookup("100.64.0.1", func() (model.Whois, error) {
		atomic.AddInt32(&resolveCount, 1)
		return who, nil
	})
	require.NoError(t, err)
	assert.Equal(t, who, result)

	time.Sleep(100 * time.Millisecond)

	who2 := model.Whois{ID: "user-2", DisplayName: "Bob"}
	result2, err2 := c.Lookup("100.64.0.1", func() (model.Whois, error) {
		atomic.AddInt32(&resolveCount, 1)
		return who2, nil
	})
	require.NoError(t, err2)
	assert.Equal(t, who2, result2)

	assert.Equal(t, int32(2), atomic.LoadInt32(&resolveCount))
}

func TestWhoisCache_EmptyIP(t *testing.T) {
	t.Parallel()

	c := NewWhoisCache(30 * time.Second)

	result, err := c.Lookup("", func() (model.Whois, error) {
		t.Fatal("resolve should not be called for empty IP")
		return model.Whois{}, nil
	})
	assert.NoError(t, err)
	assert.Equal(t, model.Whois{}, result)
}

func TestWhoisCache_InvalidIP(t *testing.T) {
	t.Parallel()

	c := NewWhoisCache(30 * time.Second)

	result, err := c.Lookup("not-an-ip", func() (model.Whois, error) {
		t.Fatal("resolve should not be called for invalid IP")
		return model.Whois{}, nil
	})
	assert.NoError(t, err)
	assert.Equal(t, model.Whois{}, result)
}

func TestWhoisCache_SingleflightDedup(t *testing.T) {
	t.Parallel()

	c := NewWhoisCache(30 * time.Second)

	var resolveCount int32
	var started sync.WaitGroup
	started.Add(1)

	who := model.Whois{ID: "user-1", DisplayName: "Alice"}

	var wg sync.WaitGroup
	results := make([]model.Whois, 5)
	errors := make([]error, 5)

	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			started.Wait()
			r, e := c.Lookup("100.64.0.1", func() (model.Whois, error) {
				atomic.AddInt32(&resolveCount, 1)
				time.Sleep(50 * time.Millisecond)
				return who, nil
			})
			results[idx] = r
			errors[idx] = e
		}(i)
	}

	started.Done()
	wg.Wait()

	for i := 0; i < 5; i++ {
		assert.NoError(t, errors[i], "goroutine %d should not error", i)
		assert.Equal(t, who, results[i], "goroutine %d should get the same result", i)
	}

	assert.Equal(t, int32(1), atomic.LoadInt32(&resolveCount))
}

func TestWhoisCache_ResolveError(t *testing.T) {
	t.Parallel()

	c := NewWhoisCache(30 * time.Second)

	result, err := c.Lookup("100.64.0.1", func() (model.Whois, error) {
		return model.Whois{}, assert.AnError
	})
	assert.ErrorIs(t, err, assert.AnError)
	assert.Equal(t, model.Whois{}, result)
}

func TestWhoisCache_EmptyIDNotCached(t *testing.T) {
	t.Parallel()

	c := NewWhoisCache(30 * time.Second)
	resolveCount := int32(0)

	emptyWho := model.Whois{ID: "", DisplayName: ""}

	_, _ = c.Lookup("100.64.0.1", func() (model.Whois, error) {
		atomic.AddInt32(&resolveCount, 1)
		return emptyWho, nil
	})
	_, _ = c.Lookup("100.64.0.1", func() (model.Whois, error) {
		atomic.AddInt32(&resolveCount, 1)
		return emptyWho, nil
	})

	assert.Equal(t, int32(2), atomic.LoadInt32(&resolveCount))
}

func TestWhoisCache_DifferentIPs(t *testing.T) {
	t.Parallel()

	c := NewWhoisCache(30 * time.Second)
	resolveCount := int32(0)

	who1 := model.Whois{ID: "user-1"}
	who2 := model.Whois{ID: "user-2"}

	r1, _ := c.Lookup("100.64.0.1", func() (model.Whois, error) {
		atomic.AddInt32(&resolveCount, 1)
		return who1, nil
	})
	r2, _ := c.Lookup("100.64.0.2", func() (model.Whois, error) {
		atomic.AddInt32(&resolveCount, 1)
		return who2, nil
	})

	assert.Equal(t, who1, r1)
	assert.Equal(t, who2, r2)
	assert.Equal(t, int32(2), atomic.LoadInt32(&resolveCount))
}

func TestNormalizeIP_InvalidHostPort(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "", NormalizeIP("not-an-ip:1234"))
}

func TestNormalizeIP_ValidHostPort(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "1.2.3.4", NormalizeIP("1.2.3.4:443"))
	assert.Equal(t, "::1", NormalizeIP("[::1]:443"))
}

func TestNormalizeIP_BareIP(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "100.64.0.1", NormalizeIP("100.64.0.1"))
}

func TestNormalizeIP_Empty(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "", NormalizeIP(""))
	assert.Equal(t, "", NormalizeIP("  "))
}

func TestNormalizeIP_InvalidBare(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "", NormalizeIP("not-an-ip"))
}
