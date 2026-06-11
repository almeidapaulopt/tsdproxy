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
	t.Parallel()
	c := &Data{
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
	t.Parallel()
	c := &Data{
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
	t.Parallel()
	c := &Data{
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
	t.Parallel()
	c := &Data{
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
	t.Parallel()
	c := &Data{
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
	t.Parallel()
	c := &Data{
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
	t.Parallel()
	c := &Data{
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
	t.Parallel()
	c := &Data{
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
	t.Parallel()
	c := &Data{
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
	t.Parallel()
	c := &Data{
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
	t.Parallel()
	c := &Data{
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
	t.Parallel()
	c := &Data{
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
	t.Parallel()
	c := &Data{
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
	t.Parallel()
	c := &Data{
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
	t.Parallel()
	c := &Data{
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
	t.Parallel()
	c := &Data{
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

// --- Error types ---

func TestDefaultProxyProviderNotFoundError(t *testing.T) {
	err := &DefaultProxyProviderNotFoundError{ProviderName: "my-provider"}
	assert.Contains(t, err.Error(), "my-provider")
	assert.Contains(t, err.Error(), "Default proxy provider")
}

// --- hasProxyProvider ---

func TestHasProxyProvider_Exists(t *testing.T) {
	t.Parallel()
	c := &Data{
		Tailscale: TailscaleProxyProviderConfig{
			Providers: map[string]*TailscaleServerConfig{
				"default": {},
			},
		},
	}
	assert.True(t, c.hasProxyProvider("default"))
}

func TestHasProxyProvider_CaseInsensitive(t *testing.T) {
	t.Parallel()
	c := &Data{
		Tailscale: TailscaleProxyProviderConfig{
			Providers: map[string]*TailscaleServerConfig{
				"MyProvider": {},
			},
		},
	}
	assert.True(t, c.hasProxyProvider("myprovider"))
	assert.True(t, c.hasProxyProvider("MYPROVIDER"))
	assert.True(t, c.hasProxyProvider("MyProvider"))
}

func TestHasProxyProvider_NotFound(t *testing.T) {
	t.Parallel()
	c := &Data{
		Tailscale: TailscaleProxyProviderConfig{
			Providers: map[string]*TailscaleServerConfig{
				"default": {},
			},
		},
	}
	assert.False(t, c.hasProxyProvider("nonexistent"))
}

func TestHasProxyProvider_EmptyProviders(t *testing.T) {
	t.Parallel()
	c := &Data{
		Tailscale: TailscaleProxyProviderConfig{
			Providers: map[string]*TailscaleServerConfig{},
		},
	}
	assert.False(t, c.hasProxyProvider("default"))
}

// --- getDefaultProxyProvider ---

func TestGetDefaultProxyProvider_SingleProvider(t *testing.T) {
	t.Parallel()
	c := &Data{
		Tailscale: TailscaleProxyProviderConfig{
			Providers: map[string]*TailscaleServerConfig{
				"default": {},
			},
		},
	}
	name, err := c.getDefaultProxyProvider()
	assert.NoError(t, err)
	assert.Equal(t, "default", name)
}

func TestGetDefaultProxyProvider_ReturnsFirstProvider(t *testing.T) {
	t.Parallel()
	c := &Data{
		Tailscale: TailscaleProxyProviderConfig{
			Providers: map[string]*TailscaleServerConfig{
				"alpha":   {},
				"beta":    {},
				"default": {},
			},
		},
	}
	name, err := c.getDefaultProxyProvider()
	assert.NoError(t, err)
	// Map iteration order is non-deterministic, but one of them must be returned.
	assert.Contains(t, []string{"alpha", "beta", "default"}, name)
}

func TestGetDefaultProxyProvider_NoProviders(t *testing.T) {
	t.Parallel()
	c := &Data{
		Tailscale: TailscaleProxyProviderConfig{
			Providers: map[string]*TailscaleServerConfig{},
		},
	}
	_, err := c.getDefaultProxyProvider()
	assert.ErrorIs(t, err, ErrNoDefaultProxyProvider)
}

// --- addDefaultProxyProviderToDockerProviders ---

func TestAddDefaultProxyProviderToDockerProviders_SetsDefault(t *testing.T) {
	t.Parallel()
	c := &Data{
		DefaultProxyProvider: "default-ts",
		Docker: map[string]*DockerTargetProviderConfig{
			"docker1": {},
			"docker2": {},
		},
	}
	err := c.addDefaultProxyProviderToDockerProviders()
	assert.NoError(t, err)
	assert.Equal(t, "default-ts", c.Docker["docker1"].DefaultProxyProvider)
	assert.Equal(t, "default-ts", c.Docker["docker2"].DefaultProxyProvider)
}

func TestAddDefaultProxyProviderToDockerProviders_PreservesExisting(t *testing.T) {
	t.Parallel()
	c := &Data{
		DefaultProxyProvider: "default-ts",
		Tailscale: TailscaleProxyProviderConfig{
			Providers: map[string]*TailscaleServerConfig{
				"default-ts": {},
				"custom-ts":  {},
			},
		},
		Docker: map[string]*DockerTargetProviderConfig{
			"docker1": {DefaultProxyProvider: "custom-ts"},
			"docker2": {},
		},
	}
	err := c.addDefaultProxyProviderToDockerProviders()
	assert.NoError(t, err)
	assert.Equal(t, "custom-ts", c.Docker["docker1"].DefaultProxyProvider,
		"existing provider should not be overwritten")
	assert.Equal(t, "default-ts", c.Docker["docker2"].DefaultProxyProvider)
}

func TestAddDefaultProxyProviderToDockerProviders_UnknownProviderReturnsError(t *testing.T) {
	t.Parallel()
	c := &Data{
		DefaultProxyProvider: "default-ts",
		Tailscale: TailscaleProxyProviderConfig{
			Providers: map[string]*TailscaleServerConfig{
				"default-ts": {},
			},
		},
		Docker: map[string]*DockerTargetProviderConfig{
			"docker1": {DefaultProxyProvider: "nonexistent-ts"},
		},
	}
	err := c.addDefaultProxyProviderToDockerProviders()
	assert.Error(t, err)
	assert.IsType(t, &DefaultProxyProviderNotFoundError{}, err)
}

func TestAddDefaultProxyProviderToDockerProviders_NoDockerProviders(t *testing.T) {
	t.Parallel()
	c := &Data{
		DefaultProxyProvider: "default-ts",
		Docker:               map[string]*DockerTargetProviderConfig{},
	}
	err := c.addDefaultProxyProviderToDockerProviders()
	assert.NoError(t, err)
}

// --- validateDNSProviders ---

func TestValidateDNSProviders_ValidCloudflare(t *testing.T) {
	t.Parallel()
	c := &Data{
		DNSProviders: map[string]*DNSProviderConfig{
			"cf": {Provider: "cloudflare", APIToken: "test-token"},
		},
	}
	err := c.validateDNSProviders()
	assert.NoError(t, err)
}

func TestValidateDNSProviders_ValidMagicDNS(t *testing.T) {
	t.Parallel()
	c := &Data{
		DNSProviders: map[string]*DNSProviderConfig{
			"mdns": {Provider: "magicdns"},
		},
	}
	err := c.validateDNSProviders()
	assert.NoError(t, err)
}

func TestValidateDNSProviders_CloudflareMissingToken(t *testing.T) {
	t.Parallel()
	c := &Data{
		DNSProviders: map[string]*DNSProviderConfig{
			"cf": {Provider: "cloudflare"},
		},
	}
	err := c.validateDNSProviders()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "apiToken")
}

func TestValidateDNSProviders_UnknownProvider(t *testing.T) {
	t.Parallel()
	c := &Data{
		DNSProviders: map[string]*DNSProviderConfig{
			"custom": {Provider: "route53"},
		},
	}
	err := c.validateDNSProviders()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown provider")
}

func TestValidateDNSProviders_DefaultNotFound(t *testing.T) {
	t.Parallel()
	c := &Data{
		DefaultDNSProvider: "nonexistent",
		DNSProviders: map[string]*DNSProviderConfig{
			"cf": {Provider: "cloudflare", APIToken: "token"},
		},
	}
	err := c.validateDNSProviders()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "defaultDNSProvider")
}

func TestValidateDNSProviders_NilConfig(t *testing.T) {
	t.Parallel()
	c := &Data{
		DNSProviders: map[string]*DNSProviderConfig{
			"nil-provider": nil,
		},
	}
	err := c.validateDNSProviders()
	assert.NoError(t, err, "nil DNS provider config should be skipped")
}

// --- validateTLSProviders ---

func TestValidateTLSProviders_ValidTailscale(t *testing.T) {
	t.Parallel()
	c := &Data{
		TLSProviders: map[string]*TLSProviderConfig{
			"ts": {Provider: "tailscale"},
		},
	}
	err := c.validateTLSProviders()
	assert.NoError(t, err)
}

func TestValidateTLSProviders_ACMEWithoutEmail(t *testing.T) {
	t.Parallel()
	c := &Data{
		TLSProviders: map[string]*TLSProviderConfig{
			"acme": {Provider: "acme"},
		},
	}
	err := c.validateTLSProviders()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "email")
}

func TestValidateTLSProviders_ACMEWithEmail(t *testing.T) {
	t.Parallel()
	c := &Data{
		DefaultDNSProvider: "cf",
		DNSProviders: map[string]*DNSProviderConfig{
			"cf": {Provider: "cloudflare", APIToken: "token"},
		},
		TLSProviders: map[string]*TLSProviderConfig{
			"acme": {Provider: "acme", Email: "admin@example.com"},
		},
	}
	err := c.validateTLSProviders()
	assert.NoError(t, err)
}

func TestValidateTLSProviders_ACMEWithoutDNSProvider(t *testing.T) {
	t.Parallel()
	c := &Data{
		TLSProviders: map[string]*TLSProviderConfig{
			"acme": {Provider: "acme", Email: "admin@example.com"},
		},
	}
	err := c.validateTLSProviders()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "DNS provider")
}

func TestValidateTLSProviders_ACMEWithCloudflareNoDefault(t *testing.T) {
	t.Parallel()
	c := &Data{
		DNSProviders: map[string]*DNSProviderConfig{
			"cf": {Provider: "cloudflare", APIToken: "token"},
		},
		TLSProviders: map[string]*TLSProviderConfig{
			"acme": {Provider: "acme", Email: "admin@example.com"},
		},
	}
	err := c.validateTLSProviders()
	assert.NoError(t, err, "ACME should resolve cloudflare DNS provider even without defaultDNSProvider")
}

func TestValidateTLSProviders_UnknownProvider(t *testing.T) {
	t.Parallel()
	c := &Data{
		TLSProviders: map[string]*TLSProviderConfig{
			"custom": {Provider: "zerossl"},
		},
	}
	err := c.validateTLSProviders()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown provider")
}

func TestValidateTLSProviders_DefaultNotFound(t *testing.T) {
	t.Parallel()
	c := &Data{
		DefaultTLSProvider: "nonexistent",
		TLSProviders: map[string]*TLSProviderConfig{
			"ts": {Provider: "tailscale"},
		},
	}
	err := c.validateTLSProviders()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "defaultTLSProvider")
}

func TestValidateTLSProviders_NilConfig(t *testing.T) {
	t.Parallel()
	c := &Data{
		TLSProviders: map[string]*TLSProviderConfig{
			"nil-provider": nil,
		},
	}
	err := c.validateTLSProviders()
	assert.NoError(t, err, "nil TLS provider config should be skipped")
}
