// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package tailscale

import (
	"strings"

	tailscale "tailscale.com/client/tailscale/v2"
)

// API scope constants for Tailscale OAuth clients.
const (
	ScopeDevices  = "devices:core"
	ScopeAuthKeys = "auth_keys"
	ScopeServices = "services"
)

// APIClientFactory creates Tailscale API clients with appropriate OAuth scopes.
// It centralizes client creation so all modes (per-proxy, shared, services)
// share a single source of truth for OAuth configuration.
type APIClientFactory struct {
	clientID     string
	clientSecret string
}

// NewAPIClientFactory creates a new factory with the given OAuth credentials.
// Either value may be empty; IsAvailable() returns false in that case.
func NewAPIClientFactory(clientID, clientSecret string) *APIClientFactory {
	return &APIClientFactory{
		clientID:     strings.TrimSpace(clientID),
		clientSecret: strings.TrimSpace(clientSecret),
	}
}

// IsAvailable returns true if both OAuth client ID and secret are configured.
func (f *APIClientFactory) IsAvailable() bool {
	return f.clientID != "" && f.clientSecret != ""
}

// NewClient creates a new Tailscale API client with the given OAuth scopes.
// Returns nil if OAuth credentials are not configured (IsAvailable() == false).
func (f *APIClientFactory) NewClient(scopes ...string) *tailscale.Client {
	if !f.IsAvailable() {
		return nil
	}

	return &tailscale.Client{
		Tailnet:   "-",
		UserAgent: userAgent,
		HTTP: tailscale.OAuthConfig{
			ClientID:     f.clientID,
			ClientSecret: f.clientSecret,
			Scopes:       scopes,
		}.HTTPClient(),
	}
}

// ScopesPerProxy returns the OAuth scopes needed for per-proxy and shared modes.
func ScopesPerProxy() []string {
	return []string{ScopeDevices, ScopeAuthKeys}
}

// ScopesServices returns the OAuth scopes needed for services mode
// (includes the services scope for VIP Service management).
func ScopesServices() []string {
	return []string{ScopeDevices, ScopeAuthKeys, ScopeServices}
}
