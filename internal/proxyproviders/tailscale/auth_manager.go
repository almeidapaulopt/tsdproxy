// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package tailscale

import (
	"context"
	"fmt"
	"time"

	"github.com/rs/zerolog"
	tailscale "tailscale.com/client/tailscale/v2"

	"github.com/almeidapaulopt/tsdproxy/internal/core/secretstring"
)

// AuthConfig holds the authentication configuration for auth key resolution.
type AuthConfig struct {
	ResolvedAuthKey string
	ProxyAuthKey    secretstring.SecretString
	ProviderAuthKey secretstring.SecretString
}

const apiTimeout = 30 * time.Second

// AuthManager handles auth key resolution and OAuth key generation.
type AuthManager struct {
	log        zerolog.Logger
	apiFactory *APIClientFactory
	ephemeral  bool
}

// NewAuthManager creates a new AuthManager.
func NewAuthManager(log zerolog.Logger, apiFactory *APIClientFactory, ephemeral bool) *AuthManager {
	return &AuthManager{
		log:        log,
		apiFactory: apiFactory,
		ephemeral:  ephemeral,
	}
}

// IsOAuth returns true if OAuth credentials are configured and available.
func (m *AuthManager) IsOAuth() bool {
	return m.apiFactory != nil && m.apiFactory.IsAvailable()
}

// ResolveKey resolves the auth key following the standard precedence chain:
//  1. resolved per-proxy key
//  2. per-proxy static key
//  3. OAuth one-time key (if OAuth credentials available)
//  4. provider static key
//  5. empty (interactive login)
func (m *AuthManager) ResolveKey(ctx context.Context, cfg AuthConfig, tags string) (string, error) {
	if cfg.ResolvedAuthKey != "" {
		return cfg.ResolvedAuthKey, nil
	}

	authKey := cfg.ProxyAuthKey.Value()

	if authKey == "" && m.apiFactory != nil && m.apiFactory.IsAvailable() {
		oauthKey, err := m.GenerateOAuthKey(ctx, tags)
		if err != nil {
			return "", fmt.Errorf("resolve auth key: %w", err)
		}
		authKey = oauthKey
	}

	if authKey == "" {
		authKey = cfg.ProviderAuthKey.Value()
	}

	if authKey == "" {
		m.log.Info().Msg("No auth key configured, interactive login will be required")
	}

	return authKey, nil
}

// GenerateOAuthKey creates a fresh one-time OAuth auth key.
// Returns empty string if OAuth is not configured or tags are empty.
func (m *AuthManager) GenerateOAuthKey(ctx context.Context, tags string) (string, error) {
	cleanedTags := cleanTags(tags)
	if len(cleanedTags) == 0 {
		m.log.Warn().Msg("OAuth without tags: cannot create auth key, interactive login will be required")
		return "", nil
	}

	ctx, cancel := context.WithTimeout(ctx, apiTimeout)
	defer cancel()

	tsclient := m.apiFactory.NewClient(ScopeAuthKeys)

	capabilities := tailscale.KeyCapabilities{}
	capabilities.Devices.Create.Ephemeral = m.ephemeral
	capabilities.Devices.Create.Reusable = false
	capabilities.Devices.Create.Preauthorized = true
	capabilities.Devices.Create.Tags = cleanedTags

	ckr := tailscale.CreateKeyRequest{
		Capabilities: capabilities,
		Description:  userAgent,
	}

	authkey, err := tsclient.Keys().CreateAuthKey(ctx, ckr)
	if err != nil {
		if IsAuthError(err) {
			return "", fmt.Errorf(
				"OAuth token rejected for tags %v — ensure the tag is assigned to your OAuth client "+
					"in the Tailscale admin console (Access Controls → OAuth clients) and listed in ACL tagOwners. "+
					"Original error: %w",
				capabilities.Devices.Create.Tags, err,
			)
		}
		return "", fmt.Errorf("unable to get OAuth token: %w", err)
	}

	return authkey.Key, nil
}
