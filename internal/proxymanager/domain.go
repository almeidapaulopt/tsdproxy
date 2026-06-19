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
		// Roll back the DNS record so the CNAME doesn't point at a host
		// that can't serve a valid cert. Skip when p.ctx is already
		// canceled (teardown in progress) — cleanupDomainForProxy will
		// handle it with a fresh context, and calling CleanupDNS on a
		// canceled context would just fail and log noise.
		if p.ctx.Err() == nil {
			rollbackCtx, rollbackCancel := context.WithTimeout(context.Background(), dnsRollbackTimeout)
			if cleanupErr := pm.dnsLifecycle.CleanupDNS(rollbackCtx, dnsProvider, proxyConfig.Domain); cleanupErr != nil {
				pm.log.Error().Err(cleanupErr).
					Str("domain", proxyConfig.Domain).
					Msg("dns rollback failed after tls provisioning error")
			}
			rollbackCancel()
		}
		p.setDNSStatus(dnsproviders.DNSStatusError)
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
// The goroutine exits when p.ctx is canceled (proxy Close) OR when
// p.certTrackerStop is closed (teardownProxy). The latter prevents the tracker
// from racing with cleanupDomainForProxy's TLS Cleanup call — without it, the
// tracker could call tlsProvider.GetCertificate concurrently with TLS resource
// teardown, causing undefined behavior depending on provider thread safety.
func (pm *ProxyManager) startCertExpiryTracking(p *Proxy, tlsProvider tlsproviders.Provider, domain string) {
	pm.updateCertExpiryMetric(p, tlsProvider, domain)

	interval := pm.certExpiryRefresh
	if interval <= 0 {
		interval = defaultCertExpiryRefreshInterval
	}

	p.certTrackerStop = make(chan struct{})
	p.certTrackerDone = make(chan struct{})

	p.eventsWg.Add(1)
	go func() {
		defer p.eventsWg.Done()
		defer close(p.certTrackerDone)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				pm.updateCertExpiryMetric(p, tlsProvider, domain)
			case <-p.ctx.Done():
				return
			case <-p.certTrackerStop:
				return
			}
		}
	}()
}

// stopCertTracker signals the cert expiry tracker to exit and blocks until it
// has done so. Called by teardownProxy BEFORE cleanupDomainForProxy to
// guarantee no concurrent tlsProvider.GetCertificate calls during TLS cleanup.
// Safe for concurrent callers: sync.Once ensures the stop channel is closed
// exactly once even if two teardowns race (e.g. Fix 1's hostLocks scenario).
// Safe to call when the tracker was never started (nil channels).
func (pm *ProxyManager) stopCertTracker(p *Proxy) {
	if p.certTrackerStop == nil {
		return
	}
	p.certTrackerStopOnce.Do(func() {
		close(p.certTrackerStop)
	})
	<-p.certTrackerDone
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

	// dnsRollbackTimeout bounds the DNS cleanup call made when TLS
	// provisioning fails. Matches cleanupDomainForProxy's timeout so a
	// slow DNS API can't block proxy startup for more than 30s.
	dnsRollbackTimeout = 30 * time.Second
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

	// setupWg.Add(1) was called in registerProxy before map insertion.
	// Every exit path from here must call setupWg.Done() exactly once:
	//   - prepareDomainSetup returns skip=true → caller calls Done()
	//   - otherwise the async goroutine calls Done() on completion
	if skip := pm.prepareDomainSetup(p, proxyConfig); skip {
		p.setupWg.Done()
		return
	}

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

// prepareDomainSetup runs the synchronous portion of domain setup: config
// validation and DNS/TLS provider resolution. Returns true if the caller
// should skip the async provisioning goroutine (and thus own the
// setupWg.Done() call). Returns false when ready for async provisioning.
//
// This MUST run synchronously before Start() returns so that the TLS provider
// is already set when getListenerForPort reads it (avoiding a race where the
// proxy would fall back to Tailscale certs for a custom-domain HTTPS port).
func (pm *ProxyManager) prepareDomainSetup(p *Proxy, proxyConfig *model.Config) (skip bool) {
	if err := config.ValidateProxyConfig(
		proxyConfig.Domain,
		proxyConfig.DNSProvider,
		proxyConfig.TLSProvider,
		pm.cfg.DefaultDNSProvider,
		pm.cfg.DefaultTLSProvider,
	); err != nil {
		pm.log.Error().Err(err).Str("proxy", proxyConfig.Hostname).
			Msg("invalid domain configuration, proxy starting without custom domain")
		return true
	}

	if err := pm.resolveAndSetProviders(p, proxyConfig); err != nil {
		pm.log.Error().Err(err).Str("proxy", proxyConfig.Hostname).
			Msg("domain provider resolution failed, proxy starting without custom domain")
		return true
	}

	p.mtx.RLock()
	hasTLSProvider := p.tlsProvider != nil
	p.mtx.RUnlock()
	return !hasTLSProvider
}

func extractHost(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" {
		return rawURL
	}
	return u.Host
}
