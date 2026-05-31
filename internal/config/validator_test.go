// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestValidateProxyConfig_NoDomain(t *testing.T) {
	err := ValidateProxyConfig("", "", "", "", "")
	assert.NoError(t, err)
}

func TestValidateProxyConfig_DomainWithProviders(t *testing.T) {
	err := ValidateProxyConfig("app.example.com", "cloudflare", "acme", "", "")
	assert.NoError(t, err)
}

func TestValidateProxyConfig_DomainWithDefaults(t *testing.T) {
	err := ValidateProxyConfig("app.example.com", "", "", "cloudflare", "acme")
	assert.NoError(t, err)
}

func TestValidateProxyConfig_DomainWithoutDNSProvider(t *testing.T) {
	err := ValidateProxyConfig("app.example.com", "", "acme", "", "")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "DNS")
}

func TestValidateProxyConfig_DomainWithoutTLSProvider(t *testing.T) {
	err := ValidateProxyConfig("app.example.com", "cloudflare", "", "", "")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "TLS")
}

func TestValidateProxyConfig_DomainWithoutAnyProvider(t *testing.T) {
	err := ValidateProxyConfig("app.example.com", "", "", "", "")
	assert.Error(t, err)
}

func TestDomainProviderError(t *testing.T) {
	err := &DomainProviderError{Domain: "app.example.com", FieldType: "DNS"}
	assert.Contains(t, err.Error(), "app.example.com")
	assert.Contains(t, err.Error(), "DNS")
}

func TestValidateProxyProviders_ServicesRequiresHostname(t *testing.T) {
	c := &config{
		Tailscale: TailscaleProxyProviderConfig{
			Providers: map[string]*TailscaleServerConfig{
				"svc": {
					Services: true,
					Hostname: "",
				},
			},
		},
	}

	err := c.validateProxyProviders()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "hostname")
}

func TestValidateProxyProviders_ServicesRequiresClientID(t *testing.T) {
	c := &config{
		Tailscale: TailscaleProxyProviderConfig{
			Providers: map[string]*TailscaleServerConfig{
				"svc": {
					Services: true,
					Hostname: "myhost",
					ClientID: "",
				},
			},
		},
	}

	err := c.validateProxyProviders()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "clientId")
}

func TestValidateProxyProviders_ServicesRequiresClientSecret(t *testing.T) {
	c := &config{
		Tailscale: TailscaleProxyProviderConfig{
			Providers: map[string]*TailscaleServerConfig{
				"svc": {
					Services:         true,
					Hostname:         "myhost",
					ClientID:         "id-123",
					ClientSecret:     "",
					ClientSecretFile: "",
				},
			},
		},
	}

	err := c.validateProxyProviders()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "clientSecret")
}

func TestValidateProxyProviders_ServicesAcceptsClientSecretFile(t *testing.T) {
	c := &config{
		Tailscale: TailscaleProxyProviderConfig{
			Providers: map[string]*TailscaleServerConfig{
				"svc": { //nolint:gosec // G101: test fixtures, not real credentials
					Services:         true,
					Hostname:         "myhost",
					ClientID:         "id-123",
					ClientSecret:     "",
					ClientSecretFile: "/run/secrets/ts_client_secret",
					Tags:             "tag:proxy",
				},
			},
		},
	}

	err := c.validateProxyProviders()
	assert.NoError(t, err)
}

func TestValidateProxyProviders_ServiceModeWithoutTagsIsValid(t *testing.T) {
	c := &config{
		Tailscale: TailscaleProxyProviderConfig{
			Providers: map[string]*TailscaleServerConfig{
				"svc": {
					Services:     true,
					Hostname:     "myhost",
					ClientID:     "id-123",
					ClientSecret: "secret-123",
					Tags:         "",
				},
			},
		},
	}

	err := c.validateProxyProviders()
	// Tags are not strictly required — services mode can use interactive login.
	assert.NoError(t, err)
}

func TestValidateProxyProviders_ServicesAndSharedMutualExclusion(t *testing.T) {
	c := &config{
		Tailscale: TailscaleProxyProviderConfig{
			Providers: map[string]*TailscaleServerConfig{
				"conflict": {
					Shared:   true,
					Services: true,
					Hostname: "myhost",
				},
			},
		},
	}

	err := c.validateProxyProviders()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "shared and services")
}

func TestValidateProxyProviders_ServicesValid(t *testing.T) {
	c := &config{
		Tailscale: TailscaleProxyProviderConfig{
			Providers: map[string]*TailscaleServerConfig{
				"svc": {
					Services:     true,
					Hostname:     "myhost",
					ClientID:     "id-123",
					ClientSecret: "secret-123",
					Tags:         "tag:proxy",
				},
			},
		},
	}

	err := c.validateProxyProviders()
	assert.NoError(t, err)
}
