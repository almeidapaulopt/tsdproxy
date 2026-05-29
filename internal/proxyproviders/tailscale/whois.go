// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package tailscale

import (
	"net/http"

	"tailscale.com/client/local"

	"github.com/almeidapaulopt/tsdproxy/internal/model"
)

// whoisFromLocalClient resolves the Tailscale identity for the request's
// remote address using the given local client. Returns a zero-value Whois
// if the client is nil, the lookup fails, or the peer is a tagged node.
func whoisFromLocalClient(lc *local.Client, r *http.Request) model.Whois {
	if lc == nil {
		return model.Whois{}
	}
	who, err := lc.WhoIs(r.Context(), r.RemoteAddr)
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
