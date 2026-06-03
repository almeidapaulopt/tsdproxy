// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package tailscale

import (
	"context"

	"tailscale.com/client/local"

	"github.com/almeidapaulopt/tsdproxy/internal/model"
)

// whoisFromAddr resolves the Tailscale identity for the given address
// using the local tailscaled client. addr can be "ip:port" or a bare
// Tailscale IP. Returns a zero-value Whois if the lookup fails or the
// peer is a tagged node (tagged nodes are rejected to prevent spoofing).
func whoisFromAddr(ctx context.Context, lc *local.Client, addr string) model.Whois {
	if lc == nil {
		return model.Whois{}
	}
	who, err := lc.WhoIs(ctx, addr)
	if err != nil {
		return model.Whois{}
	}

	if who.UserProfile == nil {
		return model.Whois{}
	}

	// Reject tagged nodes — their UserProfile is the pseudo-user
	// "tagged-devices", not a real user identity. Without this check,
	// any tagged container in the tailnet could spoof as a user and
	// call admin endpoints or access allowlist-gated proxies.
	if who.Node != nil && who.Node.IsTagged() {
		return model.Whois{}
	}

	return model.Whois{
		DisplayName:   who.UserProfile.DisplayName,
		Username:      who.UserProfile.LoginName,
		ID:            who.UserProfile.ID.String(),
		ProfilePicURL: who.UserProfile.ProfilePicURL,
	}
}

// cachedWhoisFromAddr resolves the Tailscale identity for the given address
// using the local tailscaled client, with TTL cache + singleflight.
// Cache hits return immediately; cache misses are deduplicated via
// singleflight so only one WhoIs RPC runs per IP at a time.
func cachedWhoisFromAddr(ctx context.Context, cache *WhoisCache, lc *local.Client, addr string) model.Whois {
	if cache == nil || lc == nil {
		return whoisFromAddr(ctx, lc, addr)
	}

	ip := model.NormalizeIP(addr)
	if ip == "" {
		return model.Whois{}
	}

	who, err := cache.Lookup(ip, func() (model.Whois, error) {
		return whoisFromAddr(ctx, lc, addr), nil
	})
	if err != nil {
		return model.Whois{}
	}
	return who
}
