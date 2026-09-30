// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package tailscale

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"tailscale.com/client/local"
	"tailscale.com/client/tailscale/apitype"
	"tailscale.com/tailcfg"

	"github.com/almeidapaulopt/tsdproxy/internal/model"
)

var _ whoisResolver = (*local.Client)(nil)

type mockWhoisClient struct {
	err      error
	resp     *apitype.WhoIsResponse
	addrSeen []string
	addrMu   sync.Mutex
	calls    int32
}

func (m *mockWhoisClient) WhoIs(_ context.Context, remoteAddr string) (*apitype.WhoIsResponse, error) {
	atomic.AddInt32(&m.calls, 1)
	m.addrMu.Lock()
	m.addrSeen = append(m.addrSeen, remoteAddr)
	m.addrMu.Unlock()
	if m.err != nil {
		return nil, m.err
	}
	return m.resp, nil
}

// addrs returns a copy of the recorded addresses in a thread-safe manner.
func (m *mockWhoisClient) addrs() []string {
	m.addrMu.Lock()
	defer m.addrMu.Unlock()
	out := make([]string, len(m.addrSeen))
	copy(out, m.addrSeen)
	return out
}

func newProfile(id tailcfg.UserID, login, display, pic string) *tailcfg.UserProfile {
	return &tailcfg.UserProfile{
		ID:            id,
		LoginName:     login,
		DisplayName:   display,
		ProfilePicURL: pic,
	}
}

func TestWhoisFromAddr_NilClient(t *testing.T) {
	t.Parallel()

	got := whoisFromAddr(context.Background(), nil, "100.64.0.1:12345")
	assert.Equal(t, model.Whois{}, got)
}

func TestWhoisFromAddr_WhoIsError(t *testing.T) {
	t.Parallel()

	lc := &mockWhoisClient{err: errors.New("peer not found")}
	got := whoisFromAddr(context.Background(), lc, "100.64.0.1:12345")
	assert.Equal(t, model.Whois{}, got)
	assert.Equal(t, int32(1), atomic.LoadInt32(&lc.calls))
}

func TestWhoisFromAddr_NilUserProfile(t *testing.T) {
	t.Parallel()

	lc := &mockWhoisClient{resp: &apitype.WhoIsResponse{
		Node:        &tailcfg.Node{},
		UserProfile: nil,
	}}
	got := whoisFromAddr(context.Background(), lc, "100.64.0.1:12345")
	assert.Equal(t, model.Whois{}, got)
}

// TestWhoisFromAddr_TaggedNodeRejected is the SECURITY-CRITICAL test:
// tagged nodes must NOT be mapped to the synthetic "tagged-devices"
// user profile, otherwise any tagged container could impersonate a
// real user when calling admin endpoints or allowlist-gated proxies.
func TestWhoisFromAddr_TaggedNodeRejected(t *testing.T) {
	t.Parallel()

	taggedNode := &tailcfg.Node{
		Tags: []string{"tag:server"},
	}
	require.True(t, taggedNode.IsTagged(), "fixture must be tagged")

	lc := &mockWhoisClient{resp: &apitype.WhoIsResponse{
		Node:        taggedNode,
		UserProfile: newProfile(0, "tagged-devices", "Tagged Devices", ""),
	}}
	got := whoisFromAddr(context.Background(), lc, "100.64.0.1:12345")
	assert.Equal(t, model.Whois{}, got, "tagged node must be rejected")
	assert.Equal(t, int32(1), atomic.LoadInt32(&lc.calls))
}

func TestWhoisFromAddr_UntaggedNodePasses(t *testing.T) {
	t.Parallel()

	untagged := &tailcfg.Node{}
	require.False(t, untagged.IsTagged())

	lc := &mockWhoisClient{resp: &apitype.WhoIsResponse{
		Node:        untagged,
		UserProfile: newProfile(42, "alice@example.com", "Alice", "https://pic/alice"),
	}}
	got := whoisFromAddr(context.Background(), lc, "100.64.0.1:443")
	assert.Equal(t, model.Whois{
		ID:            "userid:42",
		Username:      "alice@example.com",
		DisplayName:   "Alice",
		ProfilePicURL: "https://pic/alice",
	}, got)
}

func TestWhoisFromAddr_NilNodeWithProfile(t *testing.T) {
	t.Parallel()

	lc := &mockWhoisClient{resp: &apitype.WhoIsResponse{
		Node:        nil,
		UserProfile: newProfile(7, "bob@example.com", "Bob", ""),
	}}
	got := whoisFromAddr(context.Background(), lc, "100.64.0.2:443")
	assert.Equal(t, model.Whois{
		ID:          "userid:7",
		Username:    "bob@example.com",
		DisplayName: "Bob",
	}, got)
}

func TestWhoisFromAddr_HappyPath(t *testing.T) {
	t.Parallel()

	lc := &mockWhoisClient{resp: &apitype.WhoIsResponse{
		Node:        &tailcfg.Node{},
		UserProfile: newProfile(99, "carol@example.com", "Carol", "https://pic/carol"),
	}}
	got := whoisFromAddr(context.Background(), lc, "100.64.0.3:443")
	assert.Equal(t, model.Whois{
		ID:            "userid:99",
		Username:      "carol@example.com",
		DisplayName:   "Carol",
		ProfilePicURL: "https://pic/carol",
	}, got)
	assert.Equal(t, int32(1), atomic.LoadInt32(&lc.calls))
	addrs := lc.addrs()
	require.Len(t, addrs, 1)
	assert.Equal(t, "100.64.0.3:443", addrs[0])
}

func TestWhoisFromAddr_CancelledContext(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	lc := &mockWhoisClient{err: context.Canceled}
	got := whoisFromAddr(ctx, lc, "100.64.0.4:443")
	assert.Equal(t, model.Whois{}, got)
	assert.Equal(t, int32(1), atomic.LoadInt32(&lc.calls))
}

func TestCachedWhoisFromAddr_NilCacheAndNilClient(t *testing.T) {
	t.Parallel()

	got := cachedWhoisFromAddr(context.Background(), nil, nil, "100.64.0.1:12345")
	assert.Equal(t, model.Whois{}, got)
}

func TestCachedWhoisFromAddr_NilClient(t *testing.T) {
	t.Parallel()

	cache := NewWhoisCache(30 * time.Second)
	got := cachedWhoisFromAddr(context.Background(), cache, nil, "100.64.0.1:12345")
	assert.Equal(t, model.Whois{}, got)
}

func TestCachedWhoisFromAddr_InvalidAddr(t *testing.T) {
	t.Parallel()

	cache := NewWhoisCache(30 * time.Second)
	lc := &mockWhoisClient{
		resp: &apitype.WhoIsResponse{
			UserProfile: newProfile(1, "u@e.com", "U", ""),
		},
	}
	got := cachedWhoisFromAddr(context.Background(), cache, lc, "")
	assert.Equal(t, model.Whois{}, got)
	assert.Equal(t, int32(0), atomic.LoadInt32(&lc.calls), "WhoIs must not be called for empty addr")
}

func TestCachedWhoisFromAddr_NilCacheDelegates(t *testing.T) {
	t.Parallel()

	lc := &mockWhoisClient{resp: &apitype.WhoIsResponse{
		UserProfile: newProfile(11, "dan@example.com", "Dan", ""),
	}}
	got := cachedWhoisFromAddr(context.Background(), nil, lc, "100.64.0.5:443")
	assert.Equal(t, model.Whois{
		ID:          "userid:11",
		Username:    "dan@example.com",
		DisplayName: "Dan",
	}, got)
	assert.Equal(t, int32(1), atomic.LoadInt32(&lc.calls))
}

func TestCachedWhoisFromAddr_CacheMissThenHit(t *testing.T) {
	t.Parallel()

	cache := NewWhoisCache(30 * time.Second)
	lc := &mockWhoisClient{resp: &apitype.WhoIsResponse{
		UserProfile: newProfile(21, "eve@example.com", "Eve", "https://pic/eve"),
	}}

	first := cachedWhoisFromAddr(context.Background(), cache, lc, "100.64.0.6:443")
	require.Equal(t, model.Whois{
		ID:            "userid:21",
		Username:      "eve@example.com",
		DisplayName:   "Eve",
		ProfilePicURL: "https://pic/eve",
	}, first)
	require.Equal(t, int32(1), atomic.LoadInt32(&lc.calls))

	second := cachedWhoisFromAddr(context.Background(), cache, lc, "100.64.0.6:443")
	assert.Equal(t, first, second)
	assert.Equal(t, int32(1), atomic.LoadInt32(&lc.calls), "second call must hit the cache")
}

func TestCachedWhoisFromAddr_TaggedNodeCachedAsNegative(t *testing.T) {
	t.Parallel()

	cache := NewWhoisCache(30 * time.Second)
	tagged := &tailcfg.Node{Tags: []string{"tag:prod"}}
	require.True(t, tagged.IsTagged())

	lc := &mockWhoisClient{resp: &apitype.WhoIsResponse{
		Node:        tagged,
		UserProfile: newProfile(0, "tagged-devices", "Tagged Devices", ""),
	}}

	first := cachedWhoisFromAddr(context.Background(), cache, lc, "100.64.0.7:443")
	assert.Equal(t, model.Whois{}, first, "tagged node must be rejected on first call")
	require.Equal(t, int32(1), atomic.LoadInt32(&lc.calls))

	second := cachedWhoisFromAddr(context.Background(), cache, lc, "100.64.0.7:443")
	assert.Equal(t, model.Whois{}, second)
	assert.Equal(t, int32(1), atomic.LoadInt32(&lc.calls), "negative result must be cached")
}

func TestCachedWhoisFromAddr_DifferentIPsBothFetched(t *testing.T) {
	t.Parallel()

	cache := NewWhoisCache(30 * time.Second)
	lc := &mockWhoisClient{resp: &apitype.WhoIsResponse{
		UserProfile: newProfile(33, "frank@example.com", "Frank", ""),
	}}

	_ = cachedWhoisFromAddr(context.Background(), cache, lc, "100.64.0.8:443")
	_ = cachedWhoisFromAddr(context.Background(), cache, lc, "100.64.0.9:443")
	assert.Equal(t, int32(2), atomic.LoadInt32(&lc.calls))
}
