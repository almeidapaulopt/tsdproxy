// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package config

import (
	"testing"

	"github.com/creasty/defaults"
	"github.com/go-playground/validator/v10"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
