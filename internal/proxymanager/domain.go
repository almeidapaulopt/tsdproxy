// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package proxymanager

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"net/url"
	"time"

	"tailscale.com/client/local"

	"github.com/almeidapaulopt/tsdproxy/internal/config"
	"github.com/almeidapaulopt/tsdproxy/internal/dnsproviders"
	"github.com/almeidapaulopt/tsdproxy/internal/model"
	"github.com/almeidapaulopt/tsdproxy/internal/tlsproviders"
	tailscaletls "github.com/almeidapaulopt/tsdproxy/internal/tlsproviders/tailscale"
)

func (pm *ProxyManager) setupDomainForProxy(p *Proxy, proxyConfig *model.Config) error {
	p.mtx.RLock()
	dnsProvider := p.dnsProvider
	tlsProvider := p.tlsProvider
	p.mtx.RUnlock()

	if tlsProvider.Name() == model.TLSProviderTailscale {
		if err := pm.configureTailscaleTLS(p, tlsProvider, proxyConfig); err != nil {
			return err
		}
	}

	// The Tailscale proxy URL is populated asynchronously by watchStatus().
	// Poll until a non-empty URL is available or the timeout is reached.
	targetHostname, err := pm.waitForProxyURL(p)
	if err != nil {
		return fmt.Errorf("waiting for proxy URL: %w", err)
	}

	p.setDNSStatus(dnsproviders.DNSStatusPending)

	if err := pm.dnsLifecycle.SetupDNS(p.ctx, dnsProvider, proxyConfig.Domain, targetHostname); err != nil {
		p.setDNSStatus(dnsproviders.DNSStatusError)
		return fmt.Errorf("dns setup: %w", err)
	}
	p.setDNSStatus(dnsproviders.DNSStatusActive)

	p.setTLSStatus(tlsproviders.TLSStatusPending)

	if err := pm.tlsLifecycle.Provision(p.ctx, tlsProvider, proxyConfig.Domain); err != nil {
		p.setTLSStatus(tlsproviders.TLSStatusError)
		return fmt.Errorf("tls provisioning: %w", err)
	}
	p.setTLSStatus(tlsproviders.TLSStatusActive)

	if pm.metrics != nil {
		pm.startCertExpiryTracking(p, tlsProvider, proxyConfig.Domain)
	}

	return nil
}

// startCertExpiryTracking records the initial cert expiry and spawns a
// background goroutine that periodically refreshes it. Without the refresh,
// tsdproxy_cert_expiry_seconds would go stale after the first cert renewal
// (Tailscale ~90d, ACME ~30d) and pin at 0 forever, falsely alerting operators.
//
// The goroutine exits when p.ctx is canceled (proxy Close).
func (pm *ProxyManager) startCertExpiryTracking(p *Proxy, tlsProvider tlsproviders.Provider, domain string) {
	pm.updateCertExpiryMetric(p, tlsProvider, domain)

	interval := pm.certExpiryRefresh
	if interval <= 0 {
		interval = defaultCertExpiryRefreshInterval
	}

	p.eventsWg.Add(1)
	go func() {
		defer p.eventsWg.Done()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				pm.updateCertExpiryMetric(p, tlsProvider, domain)
			case <-p.ctx.Done():
				return
			}
		}
	}()
}

func (pm *ProxyManager) configureTailscaleTLS(p *Proxy, tlsProvider tlsproviders.Provider, proxyConfig *model.Config) error {
	lcProvider, ok := p.providerProxy.(interface{ GetLocalClient() *local.Client })
	if !ok {
		return errors.New("tailscale tls requires a tailscale proxy provider")
	}
	lc := lcProvider.GetLocalClient()
	if lc == nil {
		return errors.New("tailscale local client not available (proxy not started?)")
	}
	tsTLS, ok := tlsProvider.(*tailscaletls.Provider)
	if !ok {
		return nil
	}
	tsTLS.SetLocalClient(lc)
	if proxyConfig.ProxyProvider == "" {
		return nil
	}
	if tsCfg := pm.cfg.Tailscale.Providers[proxyConfig.ProxyProvider]; tsCfg != nil {
		tsTLS.SetMaxConcurrency(tsCfg.MaxCertConcurrency)
	}
	return nil
}

const (
	proxyURLWaitTimeout = 60 * time.Second
	urlPollInterval     = 500 * time.Millisecond

	defaultCertExpiryRefreshInterval = time.Hour
)

func (pm *ProxyManager) waitForProxyURL(p *Proxy) (string, error) {
	if host := extractHost(p.providerProxy.GetURL()); host != "" {
		return host, nil
	}

	ticker := time.NewTicker(urlPollInterval)
	defer ticker.Stop()
	timeout := time.After(proxyURLWaitTimeout)

	for {
		select {
		case <-p.urlReady:
			return extractHost(p.providerProxy.GetURL()), nil
		case <-ticker.C:
			if host := extractHost(p.providerProxy.GetURL()); host != "" {
				return host, nil
			}
		case <-p.ctx.Done():
			return "", fmt.Errorf("context canceled while waiting for proxy URL: %w", p.ctx.Err())
		case <-timeout:
			return "", errors.New("timeout waiting for proxy URL to become available")
		}
	}
}

func (pm *ProxyManager) cleanupDomainForProxy(p *Proxy) {
	if p.Config.Domain == "" {
		return
	}

	const cleanupTimeout = 30 * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), cleanupTimeout)
	defer cancel()

	p.mtx.RLock()
	dnsProvider := p.dnsProvider
	tlsProvider := p.tlsProvider
	p.mtx.RUnlock()

	if tlsProvider != nil {
		if err := pm.tlsLifecycle.Cleanup(ctx, tlsProvider, p.Config.Domain); err != nil {
			pm.log.Error().Err(err).Str("domain", p.Config.Domain).Msg("tls cleanup failed")
		}
	}

	if dnsProvider != nil {
		if err := pm.dnsLifecycle.CleanupDNS(ctx, dnsProvider, p.Config.Domain); err != nil {
			pm.log.Error().Err(err).Str("domain", p.Config.Domain).Msg("dns cleanup failed")
		}
	}

	p.setDNSStatus(dnsproviders.DNSStatusNone)
	p.setTLSStatus(tlsproviders.TLSStatusNone)
}

func (pm *ProxyManager) updateCertExpiryMetric(p *Proxy, tlsProvider tlsproviders.Provider, domain string) {
	cert, err := tlsProvider.GetCertificate(p.ctx, domain)
	if err != nil {
		pm.log.Debug().Err(err).Str("proxy", p.Config.Hostname).Msg("cert expiry: failed to get certificate")
		return
	}
	if cert.Leaf == nil {
		if len(cert.Certificate) == 0 {
			return
		}
		leaf, err := x509.ParseCertificate(cert.Certificate[0])
		if err != nil {
			pm.log.Debug().Err(err).Str("proxy", p.Config.Hostname).Msg("cert expiry: failed to parse certificate")
			return
		}
		cert.Leaf = leaf
	}
	remaining := time.Until(cert.Leaf.NotAfter).Seconds()
	if remaining < 0 {
		remaining = 0
	}
	pm.metrics.SetCertExpirySeconds(p.Config.Hostname, remaining)
}

// closeTLSProvider releases background resources held by the TLS provider
// (e.g. certmagic's cache goroutine). Providers that don't implement
// tlsproviders.Closer are silently skipped.
func (pm *ProxyManager) closeTLSProvider(p *Proxy) {
	p.mtx.RLock()
	tls := p.tlsProvider
	p.mtx.RUnlock()

	if tls == nil {
		return
	}
	if closer, ok := tls.(tlsproviders.Closer); ok {
		if err := closer.Close(); err != nil {
			pm.log.Error().Err(err).Str("proxy", p.Config.Hostname).Msg("TLS provider close failed")
		}
	}
}

// configureProxyDomain validates domain config, resolves DNS/TLS providers,
// and launches the async domain setup goroutine for proxies with a custom domain.
func (pm *ProxyManager) configureProxyDomain(p *Proxy, proxyConfig *model.Config) {
	if proxyConfig.Domain == "" {
		return
	}

	if err := config.ValidateProxyConfig(
		proxyConfig.Domain,
		proxyConfig.DNSProvider,
		proxyConfig.TLSProvider,
		pm.cfg.DefaultDNSProvider,
		pm.cfg.DefaultTLSProvider,
	); err != nil {
		p.setupWg.Done()
		pm.log.Error().Err(err).Str("proxy", proxyConfig.Hostname).
			Msg("invalid domain configuration, proxy starting without custom domain")
		return
	}

	if err := pm.resolveAndSetProviders(p, proxyConfig); err != nil {
		p.setupWg.Done()
		pm.log.Error().Err(err).Str("proxy", proxyConfig.Hostname).
			Msg("domain provider resolution failed, proxy starting without custom domain")
		return
	}

	p.mtx.RLock()
	hasTLSProvider := p.tlsProvider != nil
	p.mtx.RUnlock()

	if !hasTLSProvider {
		p.setupWg.Done()
		return
	}

	// setupWg.Add(1) was already called in newProxy() before map insertion.
	go func() {
		defer p.setupWg.Done()
		if err := pm.setupDomainForProxy(p, p.Config); err != nil {
			p.SetDomainError(err.Error())
			pm.log.Error().Err(err).Str("proxy", p.Config.Hostname).
				Str("domain", p.Config.Domain).
				Msg("domain setup failed, proxy running without custom domain")
		}
	}()
}

func extractHost(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" {
		return rawURL
	}
	return u.Host
}
