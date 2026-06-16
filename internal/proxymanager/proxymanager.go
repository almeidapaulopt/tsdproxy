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
	"sync/atomic"
	"time"

	"github.com/caddyserver/certmagic"
	"github.com/rs/zerolog"
	"go.opentelemetry.io/otel/trace"
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
	"github.com/almeidapaulopt/tsdproxy/web"
)

type (
	ProxyList          map[string]*Proxy
	TargetProviderList map[string]targetproviders.TargetProvider
	ProxyProviderList  map[string]proxyproviders.Provider
	DNSProviderList    map[string]dnsproviders.Provider
	TLSProviderList    map[string]tlsproviders.Provider

	// ProxyManager struct stores data that is required to manage all proxies.
	//
	// Lock split (RF-2): three independent locks reduce contention under
	// high container churn by allowing provider lookups and subscriber
	// notifications to proceed concurrently with proxy map mutations.
	//
	//   proxyMu    — guards Proxies and targetIndex (hot path: container
	//                 start/stop events, dashboard reads).
	//   providerMu — guards TargetProviders, ProxyProviders, DNSProviders,
	//                 TLSProviders (relatively static after startup).
	//   subMu      — guards statusSubscribers (SSE dashboard subscriptions).
	//
	// No method acquires more than one of these locks simultaneously,
	// eliminating the possibility of lock-ordering deadlocks.
	ProxyManager struct {
		log               zerolog.Logger
		ctx               context.Context
		tracerProvider    trace.TracerProvider
		Proxies           ProxyList
		targetIndex       map[string]string
		cfg               *config.Data
		assets            *web.Assets
		TargetProviders   TargetProviderList
		ProxyProviders    ProxyProviderList
		DNSProviders      DNSProviderList
		TLSProviders      TLSProviderList
		dnsLifecycle      *dnsproviders.LifecycleManager
		tlsLifecycle      *tlsproviders.TLSLifecycleManager
		statusSubscribers map[*statusSubscription]struct{}
		webhookSender     *webhook.Sender
		metrics           *metrics.Metrics
		cancel            context.CancelFunc
		targetLocks       *keyedLocks
		hostLocks         *keyedLocks
		proxyAuthToken    string
		proxyMu           sync.RWMutex
		providerMu        sync.RWMutex
		subMu             sync.RWMutex
		stopping          atomic.Bool
	}
)

var (
	ErrProxyProviderNotFound  = errors.New("proxyProvider not found")
	ErrTargetProviderNotFound = errors.New("targetProvider not found")
	ErrNoDNSProvider          = errors.New("no dns provider configured")
	ErrNoTLSProvider          = errors.New("no tls provider configured")
)

// NewProxyManager function creates a new ProxyManager.
func NewProxyManager(logger zerolog.Logger, cfg *config.Data, proxyAuthToken string, tp trace.TracerProvider, assets *web.Assets) *ProxyManager {
	ctx, cancel := context.WithCancel(context.Background())
	pm := &ProxyManager{
		ctx:               ctx,
		cancel:            cancel,
		cfg:               cfg,
		proxyAuthToken:    proxyAuthToken,
		Proxies:           make(ProxyList),
		targetIndex:       make(map[string]string),
		TargetProviders:   make(TargetProviderList),
		ProxyProviders:    make(ProxyProviderList),
		DNSProviders:      make(DNSProviderList),
		TLSProviders:      make(TLSProviderList),
		statusSubscribers: make(map[*statusSubscription]struct{}),
		log:               logger.With().Str("module", "proxymanager").Logger(),
		metrics:           metrics.New(nil),
		tracerProvider:    tp,
		webhookSender:     webhook.NewSender(logger, cfg.Webhooks),
		assets:            assets,
		targetLocks:       newKeyedLocks(),
		hostLocks:         newKeyedLocks(),
	}

	pm.dnsLifecycle = dnsproviders.NewLifecycleManager(cfg.CleanupDNS)
	pm.tlsLifecycle = tlsproviders.NewTLSLifecycleManager(true)

	return pm
}

// Start method starts the ProxyManager.
func (pm *ProxyManager) Start() {
	if pm.webhookSender != nil {
		pm.webhookSender.Start()
	}

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

	pm.stopping.Store(true)
	pm.cancel()

	pm.proxyMu.RLock()
	ids := make([]string, 0, len(pm.Proxies))
	for id := range pm.Proxies {
		ids = append(ids, id)
	}
	pm.proxyMu.RUnlock()

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
				select {
				case <-pm.ctx.Done():
					return
				default:
				}

				ctx, cancel := context.WithCancel(pm.ctx)

				eventsChan := make(chan targetproviders.TargetEvent)
				errChan := make(chan error, 1)

				go provider.WatchEvents(ctx, eventsChan, errChan)

			streamLoop:
				for {
					select {
					case <-pm.ctx.Done():
						cancel()
						return
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

const (
	backoffMultiplier = 2
	statusSubChanBuf  = 64
)

func (pm *ProxyManager) reconnectBackoff(provider targetproviders.TargetProvider, current time.Duration, reason string) time.Duration {
	pm.log.Warn().Str("provider", provider.GetDefaultProxyProviderName()).
		Dur("retry_after", current).
		Msg(reason + ", reconnecting")
	timer := time.NewTimer(current)
	select {
	case <-pm.ctx.Done():
		if !timer.Stop() {
			<-timer.C
		}
	case <-timer.C:
	}
	next := current * backoffMultiplier
	if next > maxWatchBackoff {
		return maxWatchBackoff
	}
	return next
}

// HandleProxyEvent method handles events from a targetprovider.
// Each event is serialized per target ID so that stop/start for the same
// target cannot interleave, while different targets process in parallel.
//
// Start() runs OUTSIDE the target lock so a blocking Tailscale login does
// not prevent stop events for other targets from being processed.
func (pm *ProxyManager) HandleProxyEvent(event targetproviders.TargetEvent) {
	pm.targetLocks.Lock(event.ID)

	var proxyToStart *Proxy
	var err error

	switch event.Action {
	case targetproviders.ActionStartProxy:
		proxyToStart, err = pm.eventStart(event)
	case targetproviders.ActionStopProxy:
		pm.eventStop(event)
	case targetproviders.ActionRestartProxy:
		pm.eventStop(event)
		proxyToStart, err = pm.eventStart(event)
	default:
		pm.log.Warn().Str("targetID", event.ID).Msgf("unknown proxy event action: %d", event.Action)
	}

	pm.targetLocks.Unlock(event.ID)

	if err != nil {
		pm.log.Error().Err(err).Str("targetID", event.ID).Msg("Error processing proxy event")
		return
	}

	if proxyToStart != nil {
		// Re-check that the proxy is still in the map with the same
		// pointer identity. A concurrent stop event could have removed
		// and closed it between the target lock release and here.
		current, exists := pm.GetProxy(proxyToStart.Config.Hostname)
		if !exists || current != proxyToStart {
			pm.log.Debug().Str("targetID", event.ID).Msg("proxy removed before Start() could execute")
			return
		}

		if startErr := proxyToStart.Start(); startErr != nil {
			pm.log.Error().Err(startErr).Str("targetID", event.ID).Msg("proxy start failed")

			pm.closeAndRemoveProxy(proxyToStart.Config.Hostname, proxyToStart.Config.ProxyProvider)
		}
	}
}

type statusSubscription struct {
	ch   chan model.ProxyEvent
	once sync.Once
}

// SubscribeStatusEvents returns a channel of proxy events and a cancel function.
func (pm *ProxyManager) SubscribeStatusEvents() (<-chan model.ProxyEvent, func()) {
	sub := &statusSubscription{ch: make(chan model.ProxyEvent, statusSubChanBuf)}

	pm.subMu.Lock()
	pm.statusSubscribers[sub] = struct{}{}
	pm.subMu.Unlock()

	cancel := func() {
		sub.once.Do(func() {
			pm.subMu.Lock()
			delete(pm.statusSubscribers, sub)
			close(sub.ch)
			pm.subMu.Unlock()
		})
	}

	return sub.ch, cancel
}

func (pm *ProxyManager) GetProxies() ProxyList {
	pm.proxyMu.RLock()
	defer pm.proxyMu.RUnlock()

	return maps.Clone(pm.Proxies)
}

func (pm *ProxyManager) GetProxy(name string) (*Proxy, bool) {
	pm.proxyMu.RLock()
	defer pm.proxyMu.RUnlock()

	proxy, ok := pm.Proxies[name]

	return proxy, ok
}

// withCurrentProxy reads a proxy by name, acquires the per-target lock to
// serialize with event-loop stop/start, re-checks that the same instance is
// still in the map, then invokes fn. This prevents dashboard actions from
// racing with concurrent stop events.
func (pm *ProxyManager) withCurrentProxy(name string, fn func(*Proxy) error) error {
	pm.proxyMu.RLock()
	proxy, ok := pm.Proxies[name]
	pm.proxyMu.RUnlock()

	if !ok {
		return fmt.Errorf("proxy %s not found", name)
	}

	pm.targetLocks.Lock(proxy.Config.TargetID)
	defer pm.targetLocks.Unlock(proxy.Config.TargetID)

	pm.proxyMu.RLock()
	current, exists := pm.Proxies[name]
	pm.proxyMu.RUnlock()
	if !exists || current != proxy {
		return fmt.Errorf("proxy %s was removed before action could complete", name)
	}

	return fn(proxy)
}

// RestartProxy stops and re-creates a proxy using its current config.
func (pm *ProxyManager) RestartProxy(name string) error {
	return pm.withCurrentProxy(name, func(proxy *Proxy) error {
		cfg := proxy.Config
		if err := pm.restartProxyLocked(cfg.Hostname, cfg); err != nil {
			return fmt.Errorf("restart failed for proxy %s: %w", name, err)
		}
		return nil
	})
}

// PauseProxy pauses a running proxy by name.
func (pm *ProxyManager) PauseProxy(name string) error {
	return pm.withCurrentProxy(name, func(proxy *Proxy) error {
		return proxy.Pause()
	})
}

// ResumeProxy resumes a paused proxy by name.
func (pm *ProxyManager) ResumeProxy(name string) error {
	return pm.withCurrentProxy(name, func(proxy *Proxy) error {
		return proxy.Resume()
	})
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

	pm.subMu.RLock()
	for sub := range pm.statusSubscribers {
		select {
		case sub.ch <- event:
		default:
			pm.log.Warn().
				Str("proxy", event.ID).
				Str("status", event.Status.String()).
				Msg("status event dropped: subscriber channel full (slow consumer)")
		}
	}
	pm.subMu.RUnlock()
}

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

	return nil
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

// teardownProxy performs the full teardown sequence for a proxy:
// cancel context → wait for setup goroutines → cleanup DNS/TLS domains →
// teardownProxy performs the full teardown sequence for a proxy:
// cancel context → wait for setup goroutines → cleanup DNS/TLS domains →
// close the proxy → close TLS provider resources → cleanup metrics.
func (pm *ProxyManager) teardownProxy(p *Proxy) {
	p.cancelCtx()
	p.setupWg.Wait()
	pm.cleanupDomainForProxy(p)
	p.Close()
	pm.closeTLSProvider(p)
	pm.cleanupProxyMetrics(p.Config.Hostname)
}

// closeAndRemoveProxy closes and removes any proxy with the given hostname.
// If newProxyProvider is provided and differs from the old proxy's provider,
// a warning is logged since the old Tailscale machine may remain in the tailnet.
func (pm *ProxyManager) closeAndRemoveProxy(hostname string, newProxyProvider ...string) {
	pm.proxyMu.Lock()
	old, exists := pm.Proxies[hostname]
	if exists {
		delete(pm.Proxies, hostname)
		if old != nil && pm.targetIndex[old.Config.TargetID] == hostname {
			delete(pm.targetIndex, old.Config.TargetID)
		}
	}
	pm.proxyMu.Unlock()

	if old != nil {
		if len(newProxyProvider) > 0 && old.Config.ProxyProvider != newProxyProvider[0] {
			pm.log.Warn().
				Str("proxy", hostname).
				Str("old_provider", old.Config.ProxyProvider).
				Str("new_provider", newProxyProvider[0]).
				Msg("Proxy provider changed — the old Tailscale machine may need manual cleanup in the admin console")
		}

		pm.teardownProxy(old)
		pm.log.Debug().Str("proxy", hostname).Msg("Closed existing proxy for replacement")
	}
}

// removeProxy method removes a Proxy from the ProxyManager.
func (pm *ProxyManager) removeProxy(hostname string) {
	pm.proxyMu.Lock()
	proxy, exists := pm.Proxies[hostname]
	if !exists {
		pm.proxyMu.Unlock()
		return
	}

	delete(pm.Proxies, hostname)
	if pm.targetIndex[proxy.Config.TargetID] == hostname {
		delete(pm.targetIndex, proxy.Config.TargetID)
	}
	pm.proxyMu.Unlock()

	pm.teardownProxy(proxy)

	pm.log.Debug().Str("proxy", hostname).Msg("Removed proxy")
}

// eventStart method starts a Proxy from a event trigger
func (pm *ProxyManager) eventStart(event targetproviders.TargetEvent) (*Proxy, error) {
	pm.log.Debug().Str("targetID", event.ID).Msg("Adding target")

	pcfg, err := event.TargetProvider.AddTarget(event.ID)
	if err != nil {
		return nil, fmt.Errorf("error adding target: %w", err)
	}

	return pm.newProxy(pcfg.Hostname, pcfg)
}

// eventStop method stops a Proxy from a event trigger.
// It acquires the hostname lock BEFORE deleting from the map and re-checks
// identity after acquiring the lock. This prevents a concurrent
// restartProxyLocked from inserting a new proxy whose DNS/TLS resources
// get destroyed by this cleanup.
func (pm *ProxyManager) eventStop(event targetproviders.TargetEvent) {
	pm.log.Debug().Str("targetID", event.ID).Msg("Stopping target")

	pm.proxyMu.RLock()
	hostname := pm.targetIndex[event.ID]
	pm.proxyMu.RUnlock()

	if err := event.TargetProvider.DeleteProxy(event.ID); err != nil {
		pm.log.Debug().Err(err).Str("targetID", event.ID).Msg("Provider cleanup skipped")
	}

	if hostname == "" {
		return
	}

	pm.hostLocks.Lock(hostname)
	defer pm.hostLocks.Unlock(hostname)

	pm.proxyMu.Lock()
	proxy, exists := pm.Proxies[hostname]
	if exists && proxy.Config.TargetID == event.ID {
		delete(pm.Proxies, hostname)
		delete(pm.targetIndex, event.ID)
	} else {
		proxy = nil
	}
	pm.proxyMu.Unlock()

	if proxy != nil {
		pm.teardownProxy(proxy)
		pm.log.Debug().Str("proxy", hostname).Msg("Removed proxy")
	}
}

// restartProxyLocked creates a new proxy and starts it synchronously.
// Used only by RestartProxy (dashboard action) where holding the target lock
// during Start is acceptable. Callers must already hold the target lock.
func (pm *ProxyManager) restartProxyLocked(name string, proxyConfig *model.Config) error {
	p, err := pm.newProxy(name, proxyConfig)
	if err != nil {
		return err
	}

	if err := p.Start(); err != nil {
		pm.closeAndRemoveProxy(proxyConfig.Hostname, proxyConfig.ProxyProvider)
		return fmt.Errorf("proxy start failed: %w", err)
	}

	return nil
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

// newProxy creates a proxy, resolves providers, sets up domain resources,
// and inserts it into the map — but does NOT call Start. The caller must
// call Start() after releasing the target lock.
func (pm *ProxyManager) newProxy(name string, proxyConfig *model.Config) (*Proxy, error) {
	if pm.stopping.Load() {
		return nil, errors.New("proxy manager is shutting down")
	}

	pm.log.Debug().Str("proxy", name).Msg("Creating proxy")

	pm.hostLocks.Lock(proxyConfig.Hostname)
	defer pm.hostLocks.Unlock(proxyConfig.Hostname)

	if pm.stopping.Load() {
		return nil, errors.New("proxy manager is shutting down")
	}

	proxyProvider, err := pm.resolveProxyProvider(proxyConfig)
	if err != nil {
		return nil, err
	}

	pm.closeAndRemoveProxy(proxyConfig.Hostname, proxyConfig.ProxyProvider)

	p, err := pm.buildProxy(proxyConfig, proxyProvider)
	if err != nil {
		return nil, err
	}

	if err := pm.registerProxy(p, proxyConfig); err != nil {
		p.Close()
		return nil, err
	}

	pm.configureProxyDomain(p, proxyConfig)

	p.setMetricsReady(true)
	pm.updateProxyCount()

	pm.broadcastStatusEvents(model.ProxyEvent{
		ID:     p.Config.Hostname,
		Status: model.ProxyStatusInitializing,
	})

	return p, nil
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
	proxyConfig.Tailscale.ResolvedAuthKey = authKey

	return proxyProvider, nil
}

func (pm *ProxyManager) buildProxy(proxyConfig *model.Config, proxyProvider proxyproviders.Provider) (*Proxy, error) {
	p, err := NewProxy(ProxyParams{
		Ctx:            pm.ctx,
		Log:            pm.log,
		Config:         proxyConfig,
		ProxyProvider:  proxyProvider,
		Metrics:        pm.metrics,
		TracerProvider: pm.tracerProvider,
		HTTPPort:       pm.cfg.HTTP.Port,
		ProxyAuthToken: pm.proxyAuthToken,
	})
	if err != nil {
		return nil, fmt.Errorf("error creating proxy: %w", err)
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

	return p, nil
}

// registerProxy must increment setupWg BEFORE inserting into the maps so
// StopAllProxies can't observe the proxy without the WaitGroup being
// incremented — preventing the Add/Wait race where teardownProxy's Wait()
// returns before configureProxyDomain's Add(1).
func (pm *ProxyManager) registerProxy(p *Proxy, proxyConfig *model.Config) error {
	if proxyConfig.Domain != "" {
		p.setupWg.Add(1)
	}

	pm.proxyMu.Lock()
	defer pm.proxyMu.Unlock()

	if pm.stopping.Load() {
		return errors.New("proxy manager is shutting down")
	}
	pm.Proxies[proxyConfig.Hostname] = p
	pm.targetIndex[proxyConfig.TargetID] = proxyConfig.Hostname

	return nil
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

// updateProxyCount sets the proxy count metric to the current number of proxies.
func (pm *ProxyManager) updateProxyCount() {
	if pm.metrics == nil {
		return
	}
	pm.proxyMu.RLock()
	count := len(pm.Proxies)
	pm.proxyMu.RUnlock()
	pm.metrics.SetProxyCount(count)
}

// cleanupProxyMetrics removes Prometheus metrics and updates the proxy count.
func (pm *ProxyManager) cleanupProxyMetrics(hostname string) {
	if pm.metrics != nil {
		pm.metrics.DeleteProxyMetrics(hostname)
	}
	pm.updateProxyCount()
}

func extractHost(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" {
		return rawURL
	}
	return u.Host
}
