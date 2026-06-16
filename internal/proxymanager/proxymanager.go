// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package proxymanager

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"sync"
	"sync/atomic"

	"github.com/rs/zerolog"
	"go.opentelemetry.io/otel/trace"

	"github.com/almeidapaulopt/tsdproxy/internal/config"
	"github.com/almeidapaulopt/tsdproxy/internal/core/metrics"
	"github.com/almeidapaulopt/tsdproxy/internal/core/webhook"
	"github.com/almeidapaulopt/tsdproxy/internal/dnsproviders"
	"github.com/almeidapaulopt/tsdproxy/internal/model"
	"github.com/almeidapaulopt/tsdproxy/internal/proxyproviders"
	"github.com/almeidapaulopt/tsdproxy/internal/targetproviders"
	"github.com/almeidapaulopt/tsdproxy/internal/tlsproviders"
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
		eventsWg          sync.WaitGroup
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
func (pm *ProxyManager) Start() error {
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
		return errors.New("no proxy providers found")
	}

	if len(pm.TargetProviders) == 0 {
		return errors.New("no target providers found")
	}

	return nil
}

// StopAllProxies method shuts down all proxies.
func (pm *ProxyManager) StopAllProxies() {
	pm.log.Info().Msg("Shutdown all proxies")

	pm.stopping.Store(true)
	pm.cancel()
	pm.eventsWg.Wait()

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
