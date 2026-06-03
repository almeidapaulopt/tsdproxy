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

// --- autoApproveDevices: valid when services mode + OAuth configured ---

// Services mode already requires clientId+clientSecret, so the autoApproveDevices
// OAuth check is defense-in-depth. Test that valid services+OAuth+autoApproveDevices passes.
func TestValidateProxyProviders_AutoApproveDevicesValidWithOAuth(t *testing.T) {
	c := &config{
		Tailscale: TailscaleProxyProviderConfig{
			Providers: map[string]*TailscaleServerConfig{
				"svc": {
					Services:           true,
					Hostname:           "myhost",
					ClientID:           "id-123",
					ClientSecret:       "secret-123",
					Tags:               "tag:proxy",
					AutoApproveDevices: true,
				},
			},
		},
	}
	err := c.validateProxyProviders()
	assert.NoError(t, err)
}

// --- autoRemoveConflicts: valid when services mode + OAuth configured ---

// Same defense-in-depth rationale as autoApproveDevices.
func TestValidateProxyProviders_AutoRemoveConflictsValidWithOAuth(t *testing.T) {
	c := &config{
		Tailscale: TailscaleProxyProviderConfig{
			Providers: map[string]*TailscaleServerConfig{
				"svc": {
					Services:            true,
					Hostname:            "myhost",
					ClientID:            "id-123",
					ClientSecret:        "secret-123",
					Tags:                "tag:proxy",
					AutoRemoveConflicts: true,
				},
			},
		},
	}
	err := c.validateProxyProviders()
	assert.NoError(t, err)
}

// --- reconcileInterval: error without OAuth or preventDuplicates ---

func TestValidateProxyProviders_ReconcileIntervalRequiresOAuthOrPreventDuplicates(t *testing.T) {
	c := &config{
		Tailscale: TailscaleProxyProviderConfig{
			Providers: map[string]*TailscaleServerConfig{
				"default": {
					ReconcileInterval: "5m",
				},
			},
		},
	}
	err := c.validateProxyProviders()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "reconcileInterval")
}

func TestValidateProxyProviders_ReconcileIntervalValidWithOAuth(t *testing.T) {
	c := &config{
		Tailscale: TailscaleProxyProviderConfig{
			Providers: map[string]*TailscaleServerConfig{
				"default": {
					ReconcileInterval: "5m",
					ClientID:          "id-123",
					ClientSecret:      "secret-123",
				},
			},
		},
	}
	err := c.validateProxyProviders()
	assert.NoError(t, err)
}

func TestValidateProxyProviders_ReconcileIntervalValidWithPreventDuplicates(t *testing.T) {
	c := &config{
		Tailscale: TailscaleProxyProviderConfig{
			Providers: map[string]*TailscaleServerConfig{
				"default": {
					ReconcileInterval: "5m",
					PreventDuplicates: true,
				},
			},
		},
	}
	err := c.validateProxyProviders()
	assert.NoError(t, err)
}

// --- authKey + OAuth both set: warning only ---

func TestValidateProxyProviders_AuthKeyAndOAuthBothSetIsWarning(t *testing.T) {
	c := &config{
		Tailscale: TailscaleProxyProviderConfig{
			Providers: map[string]*TailscaleServerConfig{
				"default": {
					AuthKey:      "tskey-auth-xxx",
					ClientID:     "id-123",
					ClientSecret: "secret-123",
				},
			},
		},
	}
	// Warning only — validation should pass.
	err := c.validateProxyProviders()
	assert.NoError(t, err)
}

// --- autoApproveDevices/autoRemoveConflicts outside services mode: warning only ---

func TestValidateProxyProviders_AutoApproveDevicesOutsideServicesIsWarning(t *testing.T) {
	c := &config{
		Tailscale: TailscaleProxyProviderConfig{
			Providers: map[string]*TailscaleServerConfig{
				"default": {
					AutoApproveDevices: true,
				},
			},
		},
	}
	// Warning only — validation should pass.
	err := c.validateProxyProviders()
	assert.NoError(t, err)
}

func TestValidateProxyProviders_AutoRemoveConflictsOutsideServicesIsWarning(t *testing.T) {
	c := &config{
		Tailscale: TailscaleProxyProviderConfig{
			Providers: map[string]*TailscaleServerConfig{
				"default": {
					AutoRemoveConflicts: true,
				},
			},
		},
	}
	// Warning only — validation should pass.
	err := c.validateProxyProviders()
	assert.NoError(t, err)
}

// --- reconcileInterval without preventDuplicates: warning only ---

func TestValidateProxyProviders_ReconcileIntervalWithoutPreventDuplicatesIsWarning(t *testing.T) {
	c := &config{
		Tailscale: TailscaleProxyProviderConfig{
			Providers: map[string]*TailscaleServerConfig{
				"default": {
					ReconcileInterval: "5m",
					ClientID:          "id-123",
					ClientSecret:      "secret-123",
					// preventDuplicates is false (default)
				},
			},
		},
	}
	// Warning only — validation should pass.
	err := c.validateProxyProviders()
	assert.NoError(t, err)
}

// --- Helper tests: hasOAuthCredentials ---

func TestHasOAuthCredentials_WithClientSecret(t *testing.T) {
	p := &TailscaleServerConfig{
		ClientID:     "id-123",
		ClientSecret: "secret-123",
	}
	assert.True(t, hasOAuthCredentials(p))
}

func TestHasOAuthCredentials_WithClientSecretFile(t *testing.T) {
	p := &TailscaleServerConfig{ //nolint:gosec
		ClientID:         "id-123",
		ClientSecretFile: "/run/secrets/ts_secret",
	}
	assert.True(t, hasOAuthCredentials(p))
}

func TestHasOAuthCredentials_NoClientID(t *testing.T) {
	p := &TailscaleServerConfig{
		ClientSecret: "secret-123",
	}
	assert.False(t, hasOAuthCredentials(p))
}

func TestHasOAuthCredentials_NoSecret(t *testing.T) {
	p := &TailscaleServerConfig{
		ClientID: "id-123",
	}
	assert.False(t, hasOAuthCredentials(p))
}

// --- Helper tests: isReconcileIntervalPositive ---

func TestIsReconcileIntervalPositive_ValidDuration(t *testing.T) {
	p := &TailscaleServerConfig{ReconcileInterval: "5m"}
	assert.True(t, isReconcileIntervalPositive(p))
}

func TestIsReconcileIntervalPositive_Zero(t *testing.T) {
	p := &TailscaleServerConfig{ReconcileInterval: "0"}
	assert.False(t, isReconcileIntervalPositive(p))
}

func TestIsReconcileIntervalPositive_EmptyString(t *testing.T) {
	p := &TailscaleServerConfig{ReconcileInterval: ""}
	assert.False(t, isReconcileIntervalPositive(p))
}

func TestIsReconcileIntervalPositive_InvalidDuration(t *testing.T) {
	p := &TailscaleServerConfig{ReconcileInterval: "not-a-duration"}
	assert.False(t, isReconcileIntervalPositive(p))
}
