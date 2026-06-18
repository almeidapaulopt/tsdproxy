// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package config

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/go-playground/validator/v10"
	"github.com/rs/zerolog"

	"github.com/almeidapaulopt/tsdproxy/internal/model"
)

type DefaultProxyProviderNotFoundError struct {
	ProviderName string
}

func (e *DefaultProxyProviderNotFoundError) Error() string {
	return "Default proxy provider " + e.ProviderName + " not found"
}

var ErrNoDefaultProxyProvider = errors.New("no default proxy provider")

type DomainProviderError struct {
	Domain    string
	FieldType string
}

func (e *DomainProviderError) Error() string {
	return fmt.Sprintf("domain %q set but %s provider not specified", e.Domain, e.FieldType)
}

// validate method  Validate configurations.
func (c *Data) validate(log zerolog.Logger) error {
	log.Info().Msg("Validating configuration...")
	validate := validator.New()

	if err := validate.Struct(c); err != nil {
		var validationErrors validator.ValidationErrors
		if errors.As(err, &validationErrors) {
			for _, e := range validationErrors {
				log.Error().Str("namespace", e.Namespace()).Str("field", e.Field()).
					Str("tag", e.Tag()).Msg("validation error")
			}
			return err
		}
	}

	if err := c.validateProxyProviders(log); err != nil {
		return err
	}

	if err := c.validateDNSProviders(); err != nil {
		return err
	}

	if err := c.validateTLSProviders(); err != nil {
		return err
	}

	// Set default Proxy Provider if not set.
	//
	if c.DefaultProxyProvider != "" {
		if !c.hasProxyProvider(c.DefaultProxyProvider) {
			return &DefaultProxyProviderNotFoundError{ProviderName: c.DefaultProxyProvider}
		}
	} else {
		var temp string
		var err error
		if temp, err = c.getDefaultProxyProvider(); err != nil {
			return err
		}
		c.DefaultProxyProvider = strings.ToLower(temp)
	}

	// add default proxy provider to docker providers
	//
	err := c.addDefaultProxyProviderToDockerProviders()
	if err != nil {
		return err
	}
	return nil
}

func (c *Data) addDefaultProxyProviderToDockerProviders() error {
	for _, p := range c.Docker {
		if p.DefaultProxyProvider == "" {
			p.DefaultProxyProvider = c.DefaultProxyProvider
		} else {
			if !c.hasProxyProvider(p.DefaultProxyProvider) {
				return &DefaultProxyProviderNotFoundError{ProviderName: p.DefaultProxyProvider}
			}
		}
	}
	return nil
}

func (c *Data) getDefaultProxyProvider() (string, error) {
	for name := range c.Tailscale.Providers {
		return strings.ToLower(name), nil
	}
	return "", ErrNoDefaultProxyProvider
}

func (c *Data) hasProxyProvider(name string) bool {
	for n := range c.Tailscale.Providers {
		if strings.EqualFold(n, name) {
			return true
		}
	}
	return false
}

// ValidateProxyConfig validates per-proxy domain/DNS/TLS provider requirements.
// Returns error if domain is set but DNS or TLS provider is missing.
func ValidateProxyConfig(domain, dnsProvider, tlsProvider, defaultDNSProvider, defaultTLSProvider string) error {
	if domain == "" {
		return nil
	}
	if dnsProvider == "" && defaultDNSProvider == "" {
		return &DomainProviderError{Domain: domain, FieldType: "DNS"}
	}
	if tlsProvider == "" && defaultTLSProvider == "" {
		return &DomainProviderError{Domain: domain, FieldType: "TLS"}
	}
	return nil
}

func hasOAuthCredentials(p *TailscaleServerConfig) bool {
	return p.ClientID != "" && (p.ClientSecret.Value() != "" || p.ClientSecretFile != "")
}

func isReconcileIntervalPositive(p *TailscaleServerConfig) bool {
	d, err := time.ParseDuration(p.ReconcileInterval)
	if err != nil {
		return false
	}
	return d > 0
}

func (c *Data) validateProxyProviders(log zerolog.Logger) error {
	if len(c.Tailscale.Providers) == 0 {
		return errors.New("no tailscale proxy providers configured")
	}
	for name, p := range c.Tailscale.Providers {
		if p == nil {
			return fmt.Errorf("tailscale provider %q has nil configuration", name)
		}
		if p.Shared && p.Services {
			return fmt.Errorf("tailscale provider %q: cannot use both shared and services mode", name)
		}
		if p.Shared {
			if p.Hostname == "" {
				return fmt.Errorf("tailscale provider %q: shared tsnet provider requires a hostname", name)
			}
			log.Info().Str("provider", name).Str("hostname", p.Hostname).Msg("shared tsnet provider configured")
		}
		if err := c.validateServicesProvider(name, p, log); err != nil {
			return err
		}
		if err := c.validateProviderOAuthFeatures(name, p, log); err != nil {
			return err
		}
	}
	return nil
}

func (c *Data) validateServicesProvider(name string, p *TailscaleServerConfig, log zerolog.Logger) error {
	if !p.Services {
		return nil
	}
	if p.Hostname == "" {
		return fmt.Errorf("tailscale provider %q: services mode requires a hostname", name)
	}
	if p.ClientID == "" {
		return fmt.Errorf("tailscale provider %q: services mode requires clientId for VIP Services API", name)
	}
	if p.ClientSecret.Value() == "" && p.ClientSecretFile == "" {
		return fmt.Errorf("tailscale provider %q: services mode requires clientSecret or clientSecretFile for VIP Services API", name)
	}
	if p.Tags == "" {
		return fmt.Errorf("tailscale provider %q: services mode requires tags — "+
			"OAuth auth keys cannot be generated without tags. "+
			"Add a \"tags\" field (e.g. \"tag:tsdproxy\") and ensure the tag is assigned to your OAuth client "+
			"in the Tailscale admin console (Access Controls → OAuth clients) and listed in ACL tagOwners",
			name)
	}

	log.Info().Str("provider", name).Str("hostname", p.Hostname).Str("tags", p.Tags).
		Msg("services tsnet provider configured")

	if p.AutoApproveDevices && !hasOAuthCredentials(p) {
		return fmt.Errorf("tailscale provider %q: autoApproveDevices requires OAuth credentials (clientId + clientSecret)", name)
	}
	if p.AutoRemoveConflicts && !hasOAuthCredentials(p) {
		return fmt.Errorf("tailscale provider %q: autoRemoveConflicts requires OAuth credentials (clientId + clientSecret)", name)
	}

	return nil
}

func (c *Data) validateProviderOAuthFeatures(name string, p *TailscaleServerConfig, log zerolog.Logger) error {
	if isReconcileIntervalPositive(p) {
		if !hasOAuthCredentials(p) && !p.PreventDuplicates {
			return fmt.Errorf("tailscale provider %q: reconcileInterval requires OAuth credentials (clientId + clientSecret) or preventDuplicates enabled", name)
		}
		if !p.PreventDuplicates {
			log.Warn().Str("provider", name).
				Msg("reconcileInterval is set but preventDuplicates is disabled; periodic reconciliation will not run in per-proxy mode")
		}
	}

	if p.AuthKey != "" && hasOAuthCredentials(p) {
		log.Warn().Str("provider", name).
			Msg("tailscale provider has both authKey and OAuth credentials (clientId/clientSecret) configured; authKey takes precedence in per-proxy mode")
	}

	if !p.Services {
		if p.AutoApproveDevices {
			log.Warn().Str("provider", name).
				Msg("autoApproveDevices has no effect outside services mode")
		}
		if p.AutoRemoveConflicts {
			log.Warn().Str("provider", name).
				Msg("autoRemoveConflicts has no effect outside services mode")
		}
	}

	return nil
}

func (c *Data) validateDNSProviders() error {
	if c.DefaultDNSProvider != "" {
		if _, ok := c.DNSProviders[c.DefaultDNSProvider]; !ok {
			return fmt.Errorf("defaultDNSProvider %q not found in dnsProviders", c.DefaultDNSProvider)
		}
	}
	for name, cfg := range c.DNSProviders {
		if cfg == nil {
			continue
		}
		switch cfg.Provider {
		case model.DNSProviderCloudflare:
			if cfg.APIToken == "" {
				return fmt.Errorf("dns provider %q: cloudflare requires an apiToken", name)
			}
		case model.DNSProviderMagicDNS:
			// no additional validation needed
		default:
			return fmt.Errorf("dns provider %q: unknown provider type %q", name, cfg.Provider)
		}
	}
	return nil
}

func (c *Data) validateTLSProviders() error {
	if c.DefaultTLSProvider != "" {
		if _, ok := c.TLSProviders[c.DefaultTLSProvider]; !ok {
			return fmt.Errorf("defaultTLSProvider %q not found in tlsProviders", c.DefaultTLSProvider)
		}
	}
	for name, cfg := range c.TLSProviders {
		if cfg == nil {
			continue
		}
		switch cfg.Provider {
		case model.TLSProviderTailscale:
			// auto-created per proxy, valid
		case model.TLSProviderACME:
			if cfg.Email == "" {
				return fmt.Errorf("tls provider %q: acme requires an email address", name)
			}
			// Verify a DNS provider capable of DNS-01 is configured
			if c.DefaultDNSProvider == "" {
				hasDNSProvider := false
				for _, dnsCfg := range c.DNSProviders {
					if dnsCfg != nil && dnsCfg.Provider == model.DNSProviderCloudflare {
						hasDNSProvider = true
						break
					}
				}
				if !hasDNSProvider {
					return fmt.Errorf(
						"tls provider %q: acme requires a DNS provider for "+
							"DNS-01 challenges (configure a cloudflare dnsProvider or set defaultDNSProvider)",
						name)
				}
			}
		default:
			return fmt.Errorf("tls provider %q: unknown provider type %q", name, cfg.Provider)
		}
	}
	return nil
}
