// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package proxymanager

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/caddyserver/certmagic"
	"github.com/rs/zerolog"
	"tailscale.com/client/local"

	"github.com/almeidapaulopt/tsdproxy/internal/config"
	"github.com/almeidapaulopt/tsdproxy/internal/core/metrics"
	"github.com/almeidapaulopt/tsdproxy/internal/core/webhook"
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

type (
	ProxyList          map[string]*Proxy
	TargetProviderList map[string]targetproviders.TargetProvider
	ProxyProviderList  map[string]proxyproviders.Provider
	DNSProviderList    map[string]dnsproviders.Provider
	TLSProviderList    map[string]tlsproviders.Provider

	// ProxyManager struct stores data that is required to manage all proxies
	ProxyManager struct {
		log               zerolog.Logger
		Proxies           ProxyList
		TargetProviders   TargetProviderList
		ProxyProviders    ProxyProviderList
		DNSProviders      DNSProviderList
		TLSProviders      TLSProviderList
		dnsLifecycle      *dnsproviders.LifecycleManager
		tlsLifecycle      *tlsproviders.TLSLifecycleManager
		statusSubscribers map[*statusSubscription]struct{}
		webhookSender     *webhook.Sender
		metrics           *metrics.Metrics
		targetMu          sync.Map
		hostMu            sync.Map
		mtx               sync.RWMutex
	}
)

var (
	ErrProxyProviderNotFound  = errors.New("proxyProvider not found")
	ErrTargetProviderNotFound = errors.New("targetProvider not found")
	ErrNoDNSProvider          = errors.New("no dns provider configured")
	ErrNoTLSProvider          = errors.New("no tls provider configured")
)

// NewProxyManager function creates a new ProxyManager.
func NewProxyManager(logger zerolog.Logger) *ProxyManager {
	pm := &ProxyManager{
		Proxies:           make(ProxyList),
		TargetProviders:   make(TargetProviderList),
		ProxyProviders:    make(ProxyProviderList),
		DNSProviders:      make(DNSProviderList),
		TLSProviders:      make(TLSProviderList),
		statusSubscribers: make(map[*statusSubscription]struct{}),
		log:               logger.With().Str("module", "proxymanager").Logger(),
		metrics:           metrics.New(),
		webhookSender:     webhook.NewSender(logger, config.Config.Webhooks),
	}

	pm.dnsLifecycle = dnsproviders.NewLifecycleManager(config.Config.CleanupDNS)
	pm.tlsLifecycle = tlsproviders.NewTLSLifecycleManager(true)

	return pm
}

// Start method starts the ProxyManager.
func (pm *ProxyManager) Start() {
	// Add Providers
	pm.addProxyProviders()
	pm.addTargetProviders()
	pm.addDNSProviders()
	pm.addTLSProviders()

	// Do not start without providers
	if len(pm.ProxyProviders) == 0 {
		pm.log.Error().Msg("No Proxy Providers found")
		return
	}

	if len(pm.TargetProviders) == 0 {
		pm.log.Error().Msg("No Target Providers found")
		return
	}
}

// StopAllProxies method shuts down all proxies.
func (pm *ProxyManager) StopAllProxies() {
	pm.log.Info().Msg("Shutdown all proxies")

	pm.mtx.RLock()
	ids := make([]string, 0, len(pm.Proxies))
	for id := range pm.Proxies {
		ids = append(ids, id)
	}
	pm.mtx.RUnlock()

	var wg sync.WaitGroup
	for _, id := range ids {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			pm.removeProxy(id)
		}(id)
	}
	wg.Wait()

	if pm.webhookSender != nil {
		pm.webhookSender.Close()
	}
}

// WatchEvents method watches for events from all target providers.
func (pm *ProxyManager) WatchEvents() {
	for _, provider := range pm.TargetProviders {
		go func(provider targetproviders.TargetProvider) {
			backoff := time.Second

			for {
				ctx, cancel := context.WithCancel(context.Background())

				eventsChan := make(chan targetproviders.TargetEvent)
				errChan := make(chan error, 1)

				go provider.WatchEvents(ctx, eventsChan, errChan)

			streamLoop:
				for {
					select {
					case event, ok := <-eventsChan:
						if !ok {
							cancel()
							backoff = pm.reconnectBackoff(provider, backoff, "event stream closed")
							break streamLoop
						}
						go pm.HandleProxyEvent(event)
						backoff = time.Second
					case err, ok := <-errChan:
						cancel()
						msg := "event stream error"
						if ok && err != nil {
							pm.log.Err(err).Str("provider", provider.GetDefaultProxyProviderName()).Msg(msg)
						}
						backoff = pm.reconnectBackoff(provider, backoff, msg)
						break streamLoop
					}
				}
			}
		}(provider)
	}
}

const maxWatchBackoff = 5 * time.Minute

func (pm *ProxyManager) reconnectBackoff(provider targetproviders.TargetProvider, current time.Duration, reason string) time.Duration {
	pm.log.Warn().Str("provider", provider.GetDefaultProxyProviderName()).
		Dur("retry_after", current).
		Msg(reason + ", reconnecting")
	time.Sleep(current)
	next := current * 2 //nolint:mnd
	if next > maxWatchBackoff {
		return maxWatchBackoff
	}
	return next
}

// HandleProxyEvent method handles events from a targetprovider.
// Each event is serialized per target ID so that stop/start for the same
// target cannot interleave, while different targets process in parallel.
func (pm *ProxyManager) HandleProxyEvent(event targetproviders.TargetEvent) {
	mu := pm.getTargetLock(event.ID)
	mu.Lock()
	defer mu.Unlock()

	switch event.Action {
	case targetproviders.ActionStartProxy:
		pm.eventStart(event)
	case targetproviders.ActionStopProxy:
		pm.eventStop(event)
		pm.targetMu.Delete(event.ID)
	case targetproviders.ActionRestartProxy:
		pm.eventStop(event)
		pm.eventStart(event)
	}
}

// getTargetLock returns a per-target-ID mutex, creating one if needed.
func (pm *ProxyManager) getTargetLock(targetID string) *sync.Mutex {
	v, _ := pm.targetMu.LoadOrStore(targetID, &sync.Mutex{})
	return v.(*sync.Mutex)
}

type statusSubscription struct {
	ch   chan model.ProxyEvent
	once sync.Once
}

// SubscribeStatusEvents returns a channel of proxy events and a cancel function.
func (pm *ProxyManager) SubscribeStatusEvents() (<-chan model.ProxyEvent, func()) {
	sub := &statusSubscription{ch: make(chan model.ProxyEvent, 64)} //nolint:mnd

	pm.mtx.Lock()
	pm.statusSubscribers[sub] = struct{}{}
	pm.mtx.Unlock()

	cancel := func() {
		sub.once.Do(func() {
			pm.mtx.Lock()
			delete(pm.statusSubscribers, sub)
			close(sub.ch)
			pm.mtx.Unlock()
		})
	}

	return sub.ch, cancel
}

func (pm *ProxyManager) GetProxies() ProxyList {
	pm.mtx.RLock()
	defer pm.mtx.RUnlock()

	return maps.Clone(pm.Proxies)
}

func (pm *ProxyManager) GetProxy(name string) (*Proxy, bool) {
	pm.mtx.RLock()
	defer pm.mtx.RUnlock()

	proxy, ok := pm.Proxies[name]

	return proxy, ok
}

// RestartProxy stops and re-creates a proxy using its current config.
func (pm *ProxyManager) RestartProxy(name string) error {
	pm.mtx.RLock()
	proxy, ok := pm.Proxies[name]
	pm.mtx.RUnlock()

	if !ok {
		return fmt.Errorf("proxy %s not found", name)
	}

	cfg := proxy.Config

	if err := pm.newAndStartProxy(cfg.Hostname, cfg); err != nil {
		return fmt.Errorf("restart failed for proxy %s: %w", name, err)
	}

	return nil
}

// PauseProxy pauses a running proxy by name.
func (pm *ProxyManager) PauseProxy(name string) error {
	pm.mtx.RLock()
	proxy, ok := pm.Proxies[name]
	pm.mtx.RUnlock()

	if !ok {
		return fmt.Errorf("proxy %s not found", name)
	}

	return proxy.Pause()
}

// ResumeProxy resumes a paused proxy by name.
func (pm *ProxyManager) ResumeProxy(name string) error {
	pm.mtx.RLock()
	proxy, ok := pm.Proxies[name]
	pm.mtx.RUnlock()

	if !ok {
		return fmt.Errorf("proxy %s not found", name)
	}

	return proxy.Resume()
}

// MetricsHandler returns an http.Handler that serves Prometheus metrics.
func (pm *ProxyManager) MetricsHandler() http.Handler {
	return pm.metrics.Handler()
}

// broadcastStatusEvents broadcasts proxy status event to all SubscribeStatusEvents
func (pm *ProxyManager) broadcastStatusEvents(event model.ProxyEvent) {
	if pm.webhookSender != nil {
		pm.webhookSender.Send(webhook.NewEvent(event.ID, event.OldStatus, event.Status))
	}

	pm.mtx.RLock()
	for sub := range pm.statusSubscribers {
		select {
		case sub.ch <- event:
		default:
		}
	}
	pm.mtx.RUnlock()
}

// addTargetProviders method adds TargetProviders from configuration file.
func (pm *ProxyManager) addTargetProviders() {
	for name, provider := range config.Config.Docker {
		p, err := docker.New(pm.log, name, provider)
		if err != nil {
			pm.log.Error().Err(err).Msg("Error creating Docker provider")
			continue
		}

		pm.addTargetProvider(p, name)
	}
	for name, file := range config.Config.Lists {
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
	for name, provider := range config.Config.Tailscale.Providers {
		if p, err := tsproxy.New(pm.log, name, provider); err != nil {
			pm.log.Error().Err(err).Msg("Error creating Tailscale provider")
		} else {
			pm.log.Debug().Str("provider", name).Msg("Created Proxy provider")
			pm.addProxyProvider(p, name)
		}
	}
}

func (pm *ProxyManager) addDNSProviders() {
	pm.DNSProviders = make(DNSProviderList)
	for name, cfg := range config.Config.DNSProviders {
		if cfg == nil {
			continue
		}
		switch cfg.Provider {
		case model.DNSProviderCloudflare:
			if cfg.APIToken == "" {
				pm.log.Error().Str("provider", name).Msg("Cloudflare DNS provider missing API token")
				continue
			}
			pm.DNSProviders[name] = cloudflaredns.New(cfg.APIToken.Value())
			pm.log.Debug().Str("provider", name).Msg("Created Cloudflare DNS provider")
		case "magicdns":
			pm.DNSProviders[name] = magicdns.New()
			pm.log.Debug().Str("provider", name).Msg("Created MagicDNS provider")
		default:
			pm.log.Error().Str("provider", name).Str("type", cfg.Provider).Msg("Unknown DNS provider type")
		}
	}
}

func (pm *ProxyManager) addTLSProviders() {
	pm.TLSProviders = make(TLSProviderList)
	for name, cfg := range config.Config.TLSProviders {
		if cfg == nil {
			continue
		}
		switch cfg.Provider {
		case model.TLSProviderTailscale:
			pm.log.Warn().Str("provider", name).
				Msg("Tailscale TLS provider is auto-created per proxy, skipping global registration")
		case model.TLSProviderACME:
			dnsProv, err := pm.resolveDNSProviderForACMELocked()
			if err != nil {
				pm.log.Error().Err(err).Str("provider", name).
					Msg("Cannot create ACME TLS provider: no DNS provider for DNS-01 challenge")
				continue
			}
			acmeProvider, err := acmetls.New(acmetls.Config{
				Email:       cfg.Email,
				CA:          cfg.CA,
				DNSProvider: dnsProv,
				CertStorage: cfg.CertStorage,
			})
			if err != nil {
				pm.log.Error().Err(err).Str("provider", name).Msg("Failed to create ACME TLS provider")
				continue
			}
			pm.TLSProviders[name] = acmeProvider
			pm.log.Info().Str("provider", name).Msg("Created ACME TLS provider")
		default:
			pm.log.Error().Str("provider", name).Str("type", cfg.Provider).Msg("Unknown TLS provider type")
		}
	}
}

func (pm *ProxyManager) resolveDNSProviderForACMELocked() (certmagic.DNSProvider, error) {
	if config.Config.DefaultDNSProvider != "" {
		p, ok := pm.DNSProviders[config.Config.DefaultDNSProvider]
		if !ok {
			return nil, fmt.Errorf("default dns provider %q not found", config.Config.DefaultDNSProvider)
		}
		cf, ok := p.(certmagic.DNSProvider)
		if !ok {
			return nil, fmt.Errorf("dns provider %q does not support ACME DNS-01 challenges", p.Name())
		}
		return cf, nil
	}

	for name, p := range pm.DNSProviders {
		if cf, ok := p.(certmagic.DNSProvider); ok {
			pm.log.Debug().Str("provider", name).Msg("Using DNS provider for ACME DNS-01")
			return cf, nil
		}
	}

	return nil, errors.New("no DNS provider capable of ACME DNS-01 (need a provider like Cloudflare)")
}

func (pm *ProxyManager) resolveDNSProvider(proxyCfg *model.Config) (dnsproviders.Provider, error) {
	pm.mtx.RLock()
	defer pm.mtx.RUnlock()

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
	if config.Config.DefaultDNSProvider != "" {
		p, ok := pm.DNSProviders[config.Config.DefaultDNSProvider]
		if !ok {
			return nil, fmt.Errorf("default dns provider %q not found", config.Config.DefaultDNSProvider)
		}
		return p, nil
	}
	return nil, ErrNoDNSProvider
}

func (pm *ProxyManager) resolveTLSProvider(proxyCfg *model.Config) (tlsproviders.Provider, error) {
	pm.mtx.RLock()
	defer pm.mtx.RUnlock()

	return pm.resolveTLSProviderLocked(proxyCfg)
}

func (pm *ProxyManager) resolveTLSProviderLocked(proxyCfg *model.Config) (tlsproviders.Provider, error) {
	name := proxyCfg.TLSProvider
	if name == "" {
		name = config.Config.DefaultTLSProvider
	}
	if name == "" {
		return nil, ErrNoTLSProvider
	}

	if name == model.TLSProviderTailscale {
		return tailscaletls.New(nil), nil
	}

	if cfg, ok := config.Config.TLSProviders[name]; ok && cfg.Provider == model.TLSProviderTailscale {
		return tailscaletls.New(nil), nil
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

	// Detect ACME from config directly, bypassing resolveTLSProvider().
	// This handles the case where addTLSProviders skipped ACME registration
	// because no default DNS provider was configured — a proxy with its own
	// dnsProvider should still get a per-proxy ACME instance.
	tlsName := proxyConfig.TLSProvider
	if tlsName == "" {
		tlsName = config.Config.DefaultTLSProvider
	}
	if tlsCfg, ok := config.Config.TLSProviders[tlsName]; ok && tlsCfg.Provider == model.TLSProviderACME {
		certmagicDNS, ok := dnsProvider.(certmagic.DNSProvider)
		if !ok {
			return fmt.Errorf("dns provider %q does not support ACME DNS-01 challenges", dnsProvider.Name())
		}
		perProxyACME, acmeErr := acmetls.New(acmetls.Config{
			Email:       tlsCfg.Email,
			CA:          tlsCfg.CA,
			DNSProvider: certmagicDNS,
			CertStorage: tlsCfg.CertStorage,
		})
		if acmeErr != nil {
			return fmt.Errorf("failed to create per-proxy ACME TLS provider: %w", acmeErr)
		}
		p.SetDNSAndTLSProviders(dnsProvider, perProxyACME)
		return nil
	}

	tlsProvider, err := pm.resolveTLSProvider(proxyConfig)
	if err != nil {
		return fmt.Errorf("tls provider resolution: %w", err)
	}

	p.SetDNSAndTLSProviders(dnsProvider, tlsProvider)
	return nil
}

func (pm *ProxyManager) setupDomainForProxy(p *Proxy, proxyConfig *model.Config) error {
	p.mtx.RLock()
	dnsProvider := p.dnsProvider
	tlsProvider := p.tlsProvider
	p.mtx.RUnlock()

	if tlsProvider.Name() == model.TLSProviderTailscale {
		lcProvider, ok := p.providerProxy.(interface{ GetLocalClient() *local.Client })
		if !ok {
			return errors.New("tailscale tls requires a tailscale proxy provider")
		}
		lc := lcProvider.GetLocalClient()
		if lc == nil {
			return errors.New("tailscale local client not available (proxy not started?)")
		}
		if tsTLS, ok := tlsProvider.(*tailscaletls.Provider); ok {
			tsTLS.SetLocalClient(lc)
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

	return nil
}

const proxyURLWaitTimeout = 60 * time.Second

func (pm *ProxyManager) waitForProxyURL(p *Proxy) (string, error) {
	deadline := time.Now().Add(proxyURLWaitTimeout)
	ticker := time.NewTicker(500 * time.Millisecond) //nolint:mnd
	defer ticker.Stop()

	for {
		targetURL := p.providerProxy.GetURL()
		targetHostname := extractHost(targetURL)
		if targetHostname != "" {
			return targetHostname, nil
		}
		if time.Now().After(deadline) {
			return "", errors.New("timeout waiting for proxy URL to become available")
		}
		select {
		case <-p.ctx.Done():
			return "", fmt.Errorf("context canceled while waiting for proxy URL: %w", p.ctx.Err())
		case <-ticker.C:
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

// addTargetProvider method adds a TargetProvider to the ProxyManager.
func (pm *ProxyManager) addTargetProvider(provider targetproviders.TargetProvider, name string) {
	pm.mtx.Lock()
	defer pm.mtx.Unlock()

	pm.TargetProviders[name] = provider
}

// addProxyProvider method adds	a ProxyProvider to the ProxyManager.
func (pm *ProxyManager) addProxyProvider(provider proxyproviders.Provider, name string) {
	pm.mtx.Lock()
	defer pm.mtx.Unlock()

	pm.ProxyProviders[strings.ToLower(name)] = provider
}

// closeAndRemoveProxy closes and removes any proxy with the given hostname.
// If newProxyProvider is provided and differs from the old proxy's provider,
// a warning is logged since the old Tailscale machine may remain in the tailnet.
func (pm *ProxyManager) closeAndRemoveProxy(hostname string, newProxyProvider ...string) {
	pm.mtx.Lock()
	old, exists := pm.Proxies[hostname]
	if exists {
		delete(pm.Proxies, hostname)
	}
	pm.mtx.Unlock()

	if old != nil {
		if len(newProxyProvider) > 0 && old.Config.ProxyProvider != newProxyProvider[0] {
			pm.log.Warn().
				Str("proxy", hostname).
				Str("old_provider", old.Config.ProxyProvider).
				Str("new_provider", newProxyProvider[0]).
				Msg("Proxy provider changed — the old Tailscale machine may need manual cleanup in the admin console")
		}

		old.setupWg.Wait()
		pm.cleanupDomainForProxy(old)
		old.Close()
		pm.cleanupProxyMetrics(hostname)
		pm.log.Debug().Str("proxy", hostname).Msg("Closed existing proxy for replacement")
	}
}

// getHostLock returns a per-hostname mutex, creating one if needed.
func (pm *ProxyManager) getHostLock(hostname string) *sync.Mutex {
	v, _ := pm.hostMu.LoadOrStore(hostname, &sync.Mutex{})
	return v.(*sync.Mutex)
}

// removeProxy method removes a Proxy from the ProxyManager.
func (pm *ProxyManager) removeProxy(hostname string) {
	pm.mtx.Lock()
	proxy, exists := pm.Proxies[hostname]
	if !exists {
		pm.mtx.Unlock()
		return
	}

	delete(pm.Proxies, hostname)
	pm.mtx.Unlock()

	proxy.setupWg.Wait()
	pm.cleanupDomainForProxy(proxy)
	proxy.Close()
	pm.hostMu.Delete(hostname)
	pm.cleanupProxyMetrics(hostname)

	pm.log.Debug().Str("proxy", hostname).Msg("Removed proxy")
}

// eventStart method starts a Proxy from a event trigger
func (pm *ProxyManager) eventStart(event targetproviders.TargetEvent) {
	pm.log.Debug().Str("targetID", event.ID).Msg("Adding target")

	pcfg, err := event.TargetProvider.AddTarget(event.ID)
	if err != nil {
		pm.log.Error().Err(err).Str("targetID", event.ID).Msg("Error adding target")
		return
	}

	if err := pm.newAndStartProxy(pcfg.Hostname, pcfg); err != nil {
		pm.log.Error().Err(err).Str("targetID", event.ID).Msg("Error starting proxy")
	}
}

// eventStop method stops a Proxy from a event trigger
func (pm *ProxyManager) eventStop(event targetproviders.TargetEvent) {
	pm.log.Debug().Str("targetID", event.ID).Msg("Stopping target")

	pm.mtx.Lock()
	var proxy *Proxy
	for _, p := range pm.Proxies {
		if p.Config.TargetID == event.ID {
			proxy = p
			delete(pm.Proxies, p.Config.Hostname)
			break
		}
	}
	pm.mtx.Unlock()

	// Always clean up provider-side state, even if the proxy was already
	// removed from the map by a concurrent addProxy with the same hostname.
	if err := event.TargetProvider.DeleteProxy(event.ID); err != nil {
		pm.log.Debug().Err(err).Str("targetID", event.ID).Msg("Provider cleanup skipped")
	}

	if proxy != nil {
		proxy.setupWg.Wait()
		pm.cleanupDomainForProxy(proxy)
		proxy.Close()
		pm.cleanupProxyMetrics(proxy.Config.Hostname)
		pm.log.Debug().Str("proxy", proxy.Config.Hostname).Msg("Removed proxy")
	}
}

// newAndStartProxy method creates a new proxy and starts it.
// Order: resolve auth → close old → NewProxy → insert-map → broadcast → Start.
// The proxy is inserted into the map before Start() so the dashboard can
// display it immediately, even when Start() blocks on Tailscale auth.
// Auth resolution runs before close so transient OAuth/network failures
// don't tear down a working proxy. The old proxy must still be closed
// before NewProxy() because both share the same tsnet state directory.
func (pm *ProxyManager) newAndStartProxy(name string, proxyConfig *model.Config) error {
	pm.log.Debug().Str("proxy", name).Msg("Creating proxy")

	hmu := pm.getHostLock(proxyConfig.Hostname)
	hmu.Lock()
	defer hmu.Unlock()

	proxyProvider, err := pm.getProxyProvider(proxyConfig)
	if err != nil {
		return fmt.Errorf("error getting ProxyProvider: %w", err)
	}

	if dp, ok := proxyProvider.(proxyproviders.DomainRequiredProvider); ok && dp.IsDomainRequired() && proxyConfig.Domain == "" {
		return errors.New("proxy provider requires a domain to be set on each proxy")
	}

	// Resolve auth key before closing the old proxy. OAuth token exchange
	// is side-effect-free — if this fails, the existing proxy stays up.
	authKey, err := proxyProvider.ResolveAuthKey(proxyConfig)
	if err != nil {
		return fmt.Errorf("error resolving auth key: %w", err)
	}
	proxyConfig.Tailscale.ResolvedAuthKey = authKey

	// Close old proxy before NewProxy() — the provider's NewProxy() mutates
	// the shared state dir (cleanStaleState, saveStateMeta). The old tsnet
	// server must be fully stopped before those filesystem operations run.
	pm.closeAndRemoveProxy(proxyConfig.Hostname, proxyConfig.ProxyProvider)

	p, err := NewProxy(pm.log, proxyConfig, proxyProvider, pm.metrics)
	if err != nil {
		return fmt.Errorf("error creating proxy: %w", err)
	}

	p.onUpdate = func(event model.ProxyEvent) {
		pm.broadcastStatusEvents(event)
	}

	targetID := proxyConfig.TargetID
	targetProviderName := proxyConfig.TargetProvider
	p.reResolveConfig = func() (*model.Config, error) {
		tp, ok := pm.getTargetProvider(targetProviderName)
		if !ok {
			return nil, fmt.Errorf("target provider %q not found", targetProviderName)
		}
		return tp.ReResolve(targetID)
	}

	if proxyConfig.Domain != "" {
		if err := config.ValidateProxyConfig(
			proxyConfig.Domain,
			proxyConfig.DNSProvider,
			proxyConfig.TLSProvider,
			config.Config.DefaultDNSProvider,
			config.Config.DefaultTLSProvider,
		); err != nil {
			pm.log.Error().Err(err).Str("proxy", name).
				Msg("invalid domain configuration, proxy starting without custom domain")
		} else if err := pm.resolveAndSetProviders(p, proxyConfig); err != nil {
			pm.log.Error().Err(err).Str("proxy", name).
				Msg("domain provider resolution failed, proxy starting without custom domain")
		}
	}

	// Add proxy to map before starting so the dashboard can display it
	// immediately. Start() may block on listener creation when interactive
	// Tailscale login is required (tsnet.ListenTLS waits for Running state).
	pm.mtx.Lock()
	pm.Proxies[proxyConfig.Hostname] = p
	pm.mtx.Unlock()

	p.mtx.RLock()
	hasTLSProvider := p.tlsProvider != nil
	p.mtx.RUnlock()

	if proxyConfig.Domain != "" && hasTLSProvider {
		p.setupWg.Add(1)
		go func() {
			defer p.setupWg.Done()
			if err := pm.setupDomainForProxy(p, proxyConfig); err != nil {
				pm.log.Error().Err(err).Str("proxy", name).
					Str("domain", proxyConfig.Domain).
					Msg("domain setup failed, proxy running without custom domain")
			}
		}()
	}

	p.setMetricsReady(true)
	pm.updateProxyCount()

	pm.broadcastStatusEvents(model.ProxyEvent{
		ID:     p.Config.Hostname,
		Status: model.ProxyStatusInitializing,
	})

	if err := p.Start(); err != nil {
		pm.closeAndRemoveProxy(proxyConfig.Hostname, proxyConfig.ProxyProvider)
		return fmt.Errorf("proxy start failed: %w", err)
	}

	return nil
}

// getProxyProvider method returns a ProxyProvider.
func (pm *ProxyManager) getProxyProvider(proxy *model.Config) (proxyproviders.Provider, error) {
	pm.mtx.RLock()
	defer pm.mtx.RUnlock()

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

	if p, ok := pm.ProxyProviders[strings.ToLower(config.Config.DefaultProxyProvider)]; ok {
		return p, nil
	}

	return nil, ErrProxyProviderNotFound
}

func (pm *ProxyManager) getTargetProvider(name string) (targetproviders.TargetProvider, bool) {
	pm.mtx.RLock()
	defer pm.mtx.RUnlock()

	tp, ok := pm.TargetProviders[name]
	return tp, ok
}

// updateProxyCount sets the proxy count metric to the current number of proxies.
func (pm *ProxyManager) updateProxyCount() {
	if pm.metrics == nil {
		return
	}
	pm.mtx.RLock()
	count := len(pm.Proxies)
	pm.mtx.RUnlock()
	pm.metrics.SetProxyCount(count)
}

// cleanupProxyMetrics removes Prometheus metrics and updates the proxy count.
func (pm *ProxyManager) cleanupProxyMetrics(hostname string) {
	if pm.metrics != nil {
		pm.metrics.DeleteProxyMetrics(hostname)
	}
	pm.updateProxyCount()
}

// ReloadProviders rebuilds DNS and TLS provider registries from current config.
func (pm *ProxyManager) ReloadProviders() {
	pm.mtx.Lock()
	defer pm.mtx.Unlock()

	pm.addDNSProviders()
	pm.addTLSProviders()

	pm.dnsLifecycle = dnsproviders.NewLifecycleManager(config.Config.CleanupDNS)
	pm.tlsLifecycle = tlsproviders.NewTLSLifecycleManager(true)

	pm.log.Info().Msg("Reloaded DNS and TLS providers from config")
}

func extractHost(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" {
		return rawURL
	}
	return u.Host
}
