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
)

var testValidate = validator.New()

func TestConfig_Defaults(t *testing.T) {
	t.Parallel()
	c := &Data{}
	require.NoError(t, defaults.Set(c))
	assert.Equal(t, "default", c.DefaultProxyProvider)
	assert.True(t, c.CleanupDNS)
	assert.True(t, c.CleanupTLS)
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

	c := &Data{
		Tailscale: TailscaleProxyProviderConfig{
			Providers: make(map[string]*TailscaleServerConfig),
		},
		Docker:       make(map[string]*DockerTargetProviderConfig),
		Lists:        make(map[string]*ListTargetProviderConfig),
		Presets:      make(map[string]*PresetConfig),
		DNSProviders: make(map[string]*DNSProviderConfig),
		TLSProviders: make(map[string]*TLSProviderConfig),
	}
	fileConfig := NewConfigFile(zerolog.Nop(), path, c)
	err := c.loadConfigFile(fileConfig, path, zerolog.Nop())
	assert.NoError(t, err)
	assert.Equal(t, "default", c.DefaultProxyProvider)
	assert.Equal(t, "cloudflare", c.DNSProviders["cf"].Provider)
}

func TestLoadConfigFile_FileNotExists(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	c := &Data{
		Tailscale: TailscaleProxyProviderConfig{
			Providers: make(map[string]*TailscaleServerConfig),
		},
		Docker:       make(map[string]*DockerTargetProviderConfig),
		Lists:        make(map[string]*ListTargetProviderConfig),
		DNSProviders: make(map[string]*DNSProviderConfig),
		TLSProviders: make(map[string]*TLSProviderConfig),
	}
	fileConfig := NewConfigFile(zerolog.Nop(), path, c)
	err := c.loadConfigFile(fileConfig, path, zerolog.Nop())
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

	c := &Data{
		Tailscale: TailscaleProxyProviderConfig{
			Providers: make(map[string]*TailscaleServerConfig),
		},
		Docker:       make(map[string]*DockerTargetProviderConfig),
		Lists:        make(map[string]*ListTargetProviderConfig),
		DNSProviders: make(map[string]*DNSProviderConfig),
		TLSProviders: make(map[string]*TLSProviderConfig),
	}
	fileConfig := NewConfigFile(zerolog.Nop(), path, c)
	err := c.loadConfigFile(fileConfig, path, zerolog.Nop())
	assert.Error(t, err, "should error with invalid YAML content")
}

func TestLoadConfigFile_Presets(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	yamlContent := "" +
		"defaultProxyProvider: default\n" +
		"presets:\n" +
		"  gitea:\n" +
		"    ports:\n" +
		"      \"443/https:3000/http\": {}\n" +
		"    dashboard:\n" +
		"      icon: gitea\n" +
		"      category: Development\n" +
		"  insecure-app:\n" +
		"    ports:\n" +
		"      \"443/https:8443/https\":\n" +
		"        noTLSValidate: true\n" +
		"        funnel: true\n" +
		"    dashboard:\n" +
		"      visible: false\n"
	require.NoError(t, os.WriteFile(path, []byte(yamlContent), 0o600))

	c := &Data{
		Tailscale: TailscaleProxyProviderConfig{
			Providers: make(map[string]*TailscaleServerConfig),
		},
		Docker:       make(map[string]*DockerTargetProviderConfig),
		Lists:        make(map[string]*ListTargetProviderConfig),
		Presets:      make(map[string]*PresetConfig),
		DNSProviders: make(map[string]*DNSProviderConfig),
		TLSProviders: make(map[string]*TLSProviderConfig),
	}
	fileConfig := NewConfigFile(zerolog.Nop(), path, c)
	require.NoError(t, c.loadConfigFile(fileConfig, path, zerolog.Nop()))

	gitea, ok := c.Presets["gitea"]
	require.True(t, ok, "gitea preset should load")
	assert.Equal(t, map[string]*PresetPort{
		"443/https:3000/http": {},
	}, gitea.Ports)
	assert.Equal(t, "gitea", gitea.Dashboard.Icon)
	assert.Equal(t, "Development", gitea.Dashboard.Category)
	assert.Empty(t, gitea.Dashboard.Label, "unset dashboard fields must stay zero")

	insecure, ok := c.Presets["insecure-app"]
	require.True(t, ok, "insecure-app preset should load")
	port := insecure.Ports["443/https:8443/https"]
	if assert.NotNil(t, port) {
		assert.True(t, port.NoTLSValidate)
		assert.True(t, port.Funnel)
		assert.False(t, port.NoAutoDetect)
	}
	if assert.NotNil(t, insecure.Dashboard.Visible) {
		assert.False(t, *insecure.Dashboard.Visible)
	}

	// defaults must not invent preset values (e.g. a default dashboard icon
	// would override the container's image-based icon guess).
	require.NoError(t, defaults.Set(c))
	assert.Empty(t, c.Presets["gitea"].Dashboard.Label, "unset dashboard fields must stay zero after defaults")
	assert.Equal(t, "gitea", c.Presets["gitea"].Dashboard.Icon, "explicit preset values must survive defaults")
}
