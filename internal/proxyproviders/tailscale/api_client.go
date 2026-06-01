// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package tailscale

import (
	"context"
	"fmt"
	"strings"

	tailscale "tailscale.com/client/tailscale/v2"

	"github.com/almeidapaulopt/tsdproxy/internal/core/secretstring"
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
	clientSecret secretstring.SecretString
}

// NewAPIClientFactory creates a new factory with the given OAuth credentials.
// Either value may be empty; IsAvailable() returns false in that case.
func NewAPIClientFactory(clientID string, clientSecret secretstring.SecretString) *APIClientFactory {
	return &APIClientFactory{
		clientID:     strings.TrimSpace(clientID),
		clientSecret: secretstring.SecretString(strings.TrimSpace(clientSecret.Value())),
	}
}

// IsAvailable returns true if both OAuth client ID and secret are configured.
func (f *APIClientFactory) IsAvailable() bool {
	return f.clientID != "" && f.clientSecret.Value() != ""
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
		Auth: &tailscale.OAuth{
			ClientID:     f.clientID,
			ClientSecret: f.clientSecret.Value(),
			Scopes:       scopes,
		},
	}
}

// ValidateAccess tests that OAuth credentials work by making a lightweight API
// call (listing devices). Returns a descriptive error if the credentials are
// invalid, expired, or lack the requested scopes.
func (f *APIClientFactory) ValidateAccess(ctx context.Context, scopes []string) error {
	client := f.NewClient(scopes...)
	if client == nil {
		return fmt.Errorf("OAuth credentials not configured")
	}

	if _, err := client.Devices().List(ctx); err != nil {
		return fmt.Errorf(
			"OAuth validation failed for client %q with scopes %v: %w — "+
				"verify the OAuth client exists in the Tailscale admin console "+
				"(https://login.tailscale.com/admin/settings/oauth), has the required scopes, "+
				"and that the clientSecret is correct",
			f.clientID, scopes, err,
		)
	}

	return nil
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
