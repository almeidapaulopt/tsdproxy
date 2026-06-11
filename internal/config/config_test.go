// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/creasty/defaults"
	"github.com/go-playground/validator/v10"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/almeidapaulopt/tsdproxy/internal/core/secretstring"
)

var testValidate = validator.New()

func TestConfig_Defaults(t *testing.T) {
	c := &config{}
	require.NoError(t, defaults.Set(c))
	assert.Equal(t, "default", c.DefaultProxyProvider)
	assert.True(t, c.CleanupDNS)
}

func TestDNSProviderConfig_Validation(t *testing.T) {
	tests := []struct {
		name    string
		cfg     DNSProviderConfig
		wantErr bool
	}{
		{
			name:    "cloudflare with token",
			cfg:     DNSProviderConfig{Provider: "cloudflare", APIToken: "test-token"},
			wantErr: false,
		},
		{
			name:    "magicdns no token needed",
			cfg:     DNSProviderConfig{Provider: "magicdns"},
			wantErr: false,
		},
		{
			name:    "empty provider",
			cfg:     DNSProviderConfig{Provider: ""},
			wantErr: true,
		},
		{
			name:    "invalid provider",
			cfg:     DNSProviderConfig{Provider: "route53"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := testValidate.Struct(tt.cfg)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestTLSProviderConfig_Validation(t *testing.T) {
	tests := []struct {
		name    string
		cfg     TLSProviderConfig
		wantErr bool
	}{
		{
			name:    "tailscale provider",
			cfg:     TLSProviderConfig{Provider: "tailscale"},
			wantErr: false,
		},
		{
			name:    "acme provider with email",
			cfg:     TLSProviderConfig{Provider: "acme", Email: "test@example.com"},
			wantErr: false,
		},
		{
			name:    "empty provider",
			cfg:     TLSProviderConfig{Provider: ""},
			wantErr: true,
		},
		{
			name:    "invalid provider",
			cfg:     TLSProviderConfig{Provider: "zerossl"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := testValidate.Struct(tt.cfg)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestTLSProviderConfig_Defaults(t *testing.T) {
	cfg := TLSProviderConfig{}
	require.NoError(t, defaults.Set(&cfg))
	assert.Equal(t, "https://acme-v02.api.letsencrypt.org/directory", cfg.CA)
}

// TestClearSecrets_PreservesProviderAuthKey is a regression test for a bug where
// ClearSecrets wiped provider.AuthKey before tsproxy.New captured it into the
// Client struct. Per-proxy ResolveAuthKey reads that Client copy each time a
// new container starts, so it must survive ClearSecrets.
func TestClearSecrets_PreservesProviderAuthKey(t *testing.T) {
	c := &config{}
	require.NoError(t, defaults.Set(c))

	c.Tailscale.Providers = map[string]*TailscaleServerConfig{
		"default": {
			AuthKey:    secretstring.SecretString("tskey-auth-regression"),
			ControlURL: "https://controlplane.tailscale.com",
		},
	}
	c.DNSProviders = map[string]*DNSProviderConfig{
		"cf": {Provider: "cloudflare", APIToken: "cf-token"},
	}
	c.APIKey = "api-key"

	c.ClearSecrets()

	assert.Equal(t, secretstring.SecretString("tskey-auth-regression"),
		c.Tailscale.Providers["default"].AuthKey,
		"provider AuthKey must survive ClearSecrets; it is consumed lazily by tsproxy.New → ResolveAuthKey for each new proxy")
	assert.Empty(t, c.APIKey, "APIKey is one-shot and must be cleared")
	assert.Empty(t, c.DNSProviders["cf"].APIToken, "DNS APIToken is one-shot and must be cleared")
}

func TestLoadConfigFile_FileExists(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	yamlContent := "" +
		"defaultProxyProvider: default\n" +
		"dnsProviders:\n" +
		"  cf:\n" +
		"    provider: cloudflare\n" +
		"    apiToken: test-token\n" +
		"tailscale:\n" +
		"  providers:\n" +
		"    default:\n" +
		"      controlUrl: https://controlplane.tailscale.com\n"
	require.NoError(t, os.WriteFile(path, []byte(yamlContent), 0o600))

	c := &config{
		Tailscale: TailscaleProxyProviderConfig{
			Providers: make(map[string]*TailscaleServerConfig),
		},
		Docker:       make(map[string]*DockerTargetProviderConfig),
		Lists:        make(map[string]*ListTargetProviderConfig),
		DNSProviders: make(map[string]*DNSProviderConfig),
		TLSProviders: make(map[string]*TLSProviderConfig),
	}
	fileConfig := NewConfigFile(zerolog.Nop(), path, c)
	err := c.loadConfigFile(fileConfig, path)
	assert.NoError(t, err)
	assert.Equal(t, "default", c.DefaultProxyProvider)
	assert.Equal(t, "cloudflare", c.DNSProviders["cf"].Provider)
}

func TestLoadConfigFile_FileNotExists(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	c := &config{
		Tailscale: TailscaleProxyProviderConfig{
			Providers: make(map[string]*TailscaleServerConfig),
		},
		Docker:       make(map[string]*DockerTargetProviderConfig),
		Lists:        make(map[string]*ListTargetProviderConfig),
		DNSProviders: make(map[string]*DNSProviderConfig),
		TLSProviders: make(map[string]*TLSProviderConfig),
	}
	fileConfig := NewConfigFile(zerolog.Nop(), path, c)
	err := c.loadConfigFile(fileConfig, path)
	assert.NoError(t, err)

	// Should have generated default config and saved to file
	_, err = os.Stat(path)
	assert.NoError(t, err, "default config file should have been created")

	// Verify default providers were generated
	_, hasDocker := c.Docker[DockerDefaultName]
	assert.True(t, hasDocker, "default docker provider should have been generated")

	_, hasTailscale := c.Tailscale.Providers[TailscaleDefaultProviderName]
	assert.True(t, hasTailscale, "default tailscale provider should have been generated")
}

func TestLoadConfigFile_InvalidYAML(t *testing.T) {
	// File exists but has invalid YAML → should propagate the error
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte("invalid: [unclosed yaml"), 0o600))

	c := &config{
		Tailscale: TailscaleProxyProviderConfig{
			Providers: make(map[string]*TailscaleServerConfig),
		},
		Docker:       make(map[string]*DockerTargetProviderConfig),
		Lists:        make(map[string]*ListTargetProviderConfig),
		DNSProviders: make(map[string]*DNSProviderConfig),
		TLSProviders: make(map[string]*TLSProviderConfig),
	}
	fileConfig := NewConfigFile(zerolog.Nop(), path, c)
	err := c.loadConfigFile(fileConfig, path)
	assert.Error(t, err, "should error with invalid YAML content")
}

// TestClearSecrets_PreservesClientSecret mirrors the regression covered by
// commit b0dca17 — ClientSecret is needed at runtime for OAuth operations.
func TestClearSecrets_PreservesClientSecret(t *testing.T) {
	c := &config{}
	require.NoError(t, defaults.Set(c))

	c.Tailscale.Providers = map[string]*TailscaleServerConfig{
		"default": {
			ClientID:     "id",
			ClientSecret: secretstring.SecretString("secret"),
		},
	}

	c.ClearSecrets()

	assert.Equal(t, "id", c.Tailscale.Providers["default"].ClientID)
	assert.Equal(t, secretstring.SecretString("secret"), c.Tailscale.Providers["default"].ClientSecret,
		"ClientSecret must survive ClearSecrets for runtime OAuth operations")
}
