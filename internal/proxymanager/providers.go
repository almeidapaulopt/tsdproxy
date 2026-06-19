// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package proxymanager

import (
	"errors"
	"fmt"
	"strings"

	"github.com/caddyserver/certmagic"

	"github.com/almeidapaulopt/tsdproxy/internal/config"
	"github.com/almeidapaulopt/tsdproxy/internal/core/secretstring"
	"github.com/almeidapaulopt/tsdproxy/internal/dnsproviders"
	cloudflaredns "github.com/almeidapaulopt/tsdproxy/internal/dnsproviders/cloudflare"
	magicdns "github.com/almeidapaulopt/tsdproxy/internal/dnsproviders/magicdns"
	"github.com/almeidapaulopt/tsdproxy/internal/model"
	"github.com/almeidapaulopt/tsdproxy/internal/proxyproviders"
	tsproxy "github.com/almeidapaulopt/tsdproxy/internal/proxyproviders/tailscale"
	"github.com/almeidapaulopt/tsdproxy/internal/targetproviders"
	"github.com/almeidapaulopt/tsdproxy/internal/targetproviders/docker"
	"github.com/almeidapaulopt/tsdproxy/internal/targetproviders/list"
	"github.com/almeidapaulopt/tsdproxy/internal/tlsproviders"
	acmetls "github.com/almeidapaulopt/tsdproxy/internal/tlsproviders/acme"
	tailscaletls "github.com/almeidapaulopt/tsdproxy/internal/tlsproviders/tailscale"
)

// addTargetProviders method adds TargetProviders from configuration file.
func (pm *ProxyManager) addTargetProviders() {
	for name, provider := range pm.cfg.Docker {
		p, err := docker.New(pm.log, name, provider, pm.cfg.ProxyAccessLog, pm.assets)
		if err != nil {
			pm.log.Error().Err(err).Msg("Error creating Docker provider")
			continue
		}

		pm.addTargetProvider(p, name)
	}
	for name, file := range pm.cfg.Lists {
		p, err := list.New(pm.log, name, file)
		if err != nil {
			pm.log.Error().Err(err).Msg("Error creating Files provider")
			continue
		}

		pm.addTargetProvider(p, name)
	}
}

// addProxyProviders method adds ProxyProviders from configuration file.
func (pm *ProxyManager) addProxyProviders() {
	pm.log.Debug().Msg("Setting up Tailscale Providers")
	// add Tailscale Providers
	for name, provider := range pm.cfg.Tailscale.Providers {
		if p, err := tsproxy.New(pm.log, name, provider, pm.cfg.Tailscale.DataDir); err != nil {
			pm.log.Error().Err(err).Msg("Error creating Tailscale provider")
		} else {
			p.Start()
			pm.log.Debug().Str("provider", name).Msg("Created Proxy provider")
			pm.addProxyProvider(p, name)
		}
	}
}

func (pm *ProxyManager) addDNSProviders() {
	for name, cfg := range pm.cfg.DNSProviders {
		if cfg == nil {
			continue
		}
		switch cfg.Provider {
		case model.DNSProviderCloudflare:
			if cfg.APIToken == "" {
				pm.log.Error().Str("provider", name).Msg("Cloudflare DNS provider missing API token")
				continue
			}
			pm.providerMu.Lock()
			pm.DNSProviders[name] = cloudflaredns.New(cfg.APIToken.Value())
			pm.providerMu.Unlock()
			pm.log.Debug().Str("provider", name).Msg("Created Cloudflare DNS provider")
		case model.DNSProviderMagicDNS:
			pm.providerMu.Lock()
			pm.DNSProviders[name] = magicdns.New()
			pm.providerMu.Unlock()
			pm.log.Debug().Str("provider", name).Msg("Created MagicDNS provider")
		default:
			pm.log.Error().Str("provider", name).Str("type", cfg.Provider).Msg("Unknown DNS provider type")
		}
	}
}

func (pm *ProxyManager) addTLSProviders() {
	for name, cfg := range pm.cfg.TLSProviders {
		if cfg == nil {
			continue
		}
		switch cfg.Provider {
		case model.TLSProviderTailscale:
			pm.log.Warn().Str("provider", name).
				Msg("Tailscale TLS provider is auto-created per proxy, skipping global registration")
		case model.TLSProviderACME:
			pm.log.Warn().Str("provider", name).
				Msg("ACME TLS provider is auto-created per proxy, skipping global registration")
		default:
			pm.log.Error().Str("provider", name).Str("type", cfg.Provider).Msg("Unknown TLS provider type")
		}
	}
}

// resolveTLSProviderName returns the effective TLS provider name for a proxy,
// falling back to the global default.
func (pm *ProxyManager) resolveTLSProviderName(proxyCfg *model.Config) string {
	name := proxyCfg.TLSProvider
	if name == "" {
		name = pm.cfg.DefaultTLSProvider
	}
	return name
}

// newACMEProvider creates an ACME TLS provider from a TLS config and a DNS provider
// capable of DNS-01 challenges. Shared by addTLSProviders (startup) and
// resolveAndSetProviders (per-proxy ACME bypass).
func (pm *ProxyManager) newACMEProvider(tlsCfg *config.TLSProviderConfig, dnsProv certmagic.DNSProvider) (*acmetls.Provider, error) {
	return acmetls.New(acmetls.Config{
		Email:       tlsCfg.Email,
		CA:          tlsCfg.CA,
		DNSProvider: dnsProv,
		CertStorage: tlsCfg.CertStorage,
		DataDir:     pm.cfg.Tailscale.DataDir,
	})
}

func (pm *ProxyManager) resolveDNSProvider(proxyCfg *model.Config) (dnsproviders.Provider, error) {
	pm.providerMu.RLock()
	defer pm.providerMu.RUnlock()

	return pm.resolveDNSProviderLocked(proxyCfg)
}

func (pm *ProxyManager) resolveDNSProviderLocked(proxyCfg *model.Config) (dnsproviders.Provider, error) {
	if proxyCfg.DNSProvider != "" {
		p, ok := pm.DNSProviders[proxyCfg.DNSProvider]
		if !ok {
			return nil, fmt.Errorf("dns provider %q not found", proxyCfg.DNSProvider)
		}
		return p, nil
	}
	if pm.cfg.DefaultDNSProvider != "" {
		p, ok := pm.DNSProviders[pm.cfg.DefaultDNSProvider]
		if !ok {
			return nil, fmt.Errorf("default dns provider %q not found", pm.cfg.DefaultDNSProvider)
		}
		return p, nil
	}
	return nil, ErrNoDNSProvider
}

func (pm *ProxyManager) resolveTLSProvider(proxyCfg *model.Config, dnsProvider dnsproviders.Provider) (tlsproviders.Provider, error) {
	pm.providerMu.RLock()
	defer pm.providerMu.RUnlock()

	return pm.resolveTLSProviderLocked(proxyCfg, dnsProvider)
}

func (pm *ProxyManager) resolveTLSProviderLocked(proxyCfg *model.Config, dnsProvider dnsproviders.Provider) (tlsproviders.Provider, error) {
	name := pm.resolveTLSProviderName(proxyCfg)
	if name == "" {
		return nil, ErrNoTLSProvider
	}

	if name == model.TLSProviderTailscale {
		return tailscaletls.New(nil, 0), nil
	}

	if cfg, ok := pm.cfg.TLSProviders[name]; ok && cfg.Provider == model.TLSProviderTailscale {
		return tailscaletls.New(nil, 0), nil
	}

	// ACME always creates a per-proxy instance bound to the proxy's
	// resolved DNS provider, ensuring DNS-01 challenges use the correct
	// DNS zone even when a global ACME exists with a different default.
	if tlsCfg, ok := pm.cfg.TLSProviders[name]; ok && tlsCfg.Provider == model.TLSProviderACME {
		if dnsProvider == nil {
			return nil, fmt.Errorf("ACME TLS provider %q requires a DNS provider", name)
		}
		certmagicDNS, ok := dnsProvider.(certmagic.DNSProvider)
		if !ok {
			return nil, fmt.Errorf("dns provider %q does not support ACME DNS-01 challenges", dnsProvider.Name())
		}
		provider, err := pm.newACMEProvider(tlsCfg, certmagicDNS)
		if err != nil {
			return nil, fmt.Errorf("failed to create per-proxy ACME TLS provider: %w", err)
		}
		return provider, nil
	}

	p, ok := pm.TLSProviders[name]
	if !ok {
		return nil, fmt.Errorf("tls provider %q not found", name)
	}
	return p, nil
}

func (pm *ProxyManager) resolveAndSetProviders(p *Proxy, proxyConfig *model.Config) error {
	dnsProvider, err := pm.resolveDNSProvider(proxyConfig)
	if err != nil {
		return fmt.Errorf("dns provider resolution: %w", err)
	}

	tlsProvider, err := pm.resolveTLSProvider(proxyConfig, dnsProvider)
	if err != nil {
		return fmt.Errorf("tls provider resolution: %w", err)
	}

	p.SetDNSAndTLSProviders(dnsProvider, tlsProvider)
	return nil
}

// addTargetProvider method adds a TargetProvider to the ProxyManager.
func (pm *ProxyManager) addTargetProvider(provider targetproviders.TargetProvider, name string) {
	pm.providerMu.Lock()
	defer pm.providerMu.Unlock()

	pm.TargetProviders[name] = provider
}

// addProxyProvider method adds	a ProxyProvider to the ProxyManager.
func (pm *ProxyManager) addProxyProvider(provider proxyproviders.Provider, name string) {
	pm.providerMu.Lock()
	defer pm.providerMu.Unlock()

	pm.ProxyProviders[strings.ToLower(name)] = provider
}

func (pm *ProxyManager) resolveProxyProvider(proxyConfig *model.Config) (proxyproviders.Provider, error) {
	proxyProvider, err := pm.getProxyProvider(proxyConfig)
	if err != nil {
		return nil, fmt.Errorf("error getting ProxyProvider: %w", err)
	}

	if dp, ok := proxyProvider.(proxyproviders.DomainRequiredProvider); ok && dp.IsDomainRequired() && proxyConfig.Domain == "" {
		return nil, errors.New("proxy provider requires a domain to be set on each proxy")
	}

	authKey, err := proxyProvider.ResolveAuthKey(proxyConfig)
	if err != nil {
		return nil, fmt.Errorf("error resolving auth key: %w", err)
	}
	proxyConfig.Tailscale.ResolvedAuthKey = secretstring.SecretString(authKey)

	return proxyProvider, nil
}

// getProxyProvider method returns a ProxyProvider.
func (pm *ProxyManager) getProxyProvider(proxy *model.Config) (proxyproviders.Provider, error) {
	pm.providerMu.RLock()
	defer pm.providerMu.RUnlock()

	if proxy.ProxyProvider != "" {
		p, ok := pm.ProxyProviders[strings.ToLower(proxy.ProxyProvider)]
		if !ok {
			return nil, ErrProxyProviderNotFound
		}
		return p, nil
	}

	targetProvider, ok := pm.TargetProviders[proxy.TargetProvider]
	if !ok {
		return nil, ErrTargetProviderNotFound
	}
	if p, ok := pm.ProxyProviders[strings.ToLower(targetProvider.GetDefaultProxyProviderName())]; ok {
		return p, nil
	}

	if p, ok := pm.ProxyProviders[strings.ToLower(pm.cfg.DefaultProxyProvider)]; ok {
		return p, nil
	}

	return nil, ErrProxyProviderNotFound
}

func (pm *ProxyManager) getTargetProvider(name string) (targetproviders.TargetProvider, bool) {
	pm.providerMu.RLock()
	defer pm.providerMu.RUnlock()

	tp, ok := pm.TargetProviders[name]
	return tp, ok
}
