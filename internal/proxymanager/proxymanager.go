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
	"time"

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
	//
	// Keyed locks (hostLocks, targetLocks) are per-ID ref-counted mutexes
	// (see locks.go). Lock hierarchy when both a keyed lock and a struct
	// lock are needed: keyed lock FIRST, then proxyMu/providerMu/subMu.
	// This ordering is consistent across all call sites — never reversed —
	// so no deadlock is possible between keyed and struct locks.
	ProxyManager struct {
		log               zerolog.Logger
		ctx               context.Context
		tracerProvider    trace.TracerProvider
		tlsLifecycle      *tlsproviders.TLSLifecycleManager
		webhookSender     *webhook.Sender
		cfg               *config.Data
		assets            *web.Assets
		TargetProviders   TargetProviderList
		ProxyProviders    ProxyProviderList
		DNSProviders      DNSProviderList
		TLSProviders      TLSProviderList
		dnsLifecycle      *dnsproviders.LifecycleManager
		Proxies           ProxyList
		statusSubscribers map[*statusSubscription]struct{}
		targetIndex       map[string]string
		metrics           *metrics.Metrics
		hostLocks         *keyedLocks
		cancel            context.CancelFunc
		targetLocks       *keyedLocks
		proxyAuthToken    string
		eventsWg          sync.WaitGroup
		eventHandlerWg    sync.WaitGroup
		certExpiryRefresh time.Duration
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
		certExpiryRefresh: defaultCertExpiryRefreshInterval,
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
	pm.eventHandlerWg.Wait()

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
// cancel context → wait for setup goroutines → stop cert tracker →
// cleanup DNS/TLS domains → close the proxy → close TLS provider resources →
// cleanup metrics.
func (pm *ProxyManager) teardownProxy(p *Proxy) {
	p.cancelCtx()
	p.setupWg.Wait()
	pm.stopCertTracker(p)
	pm.cleanupDomainForProxy(p)
	p.Close()
	pm.closeTLSProvider(p)
	pm.cleanupProxyMetrics(p.Config.Hostname)
}

// removeAndTeardown is the shared primitive for all proxy removal paths.
// It atomically removes the proxy at hostname from the internal maps
// if the identity predicate matches, tears it down, and returns it.
//
// identity: if non-nil, removal proceeds only when identity(current) is
// true. nil means "remove whatever is at hostname". Callers that already
// hold hostLocks (eventStop, closeProxyIfStillCurrent) must NOT acquire
// them again; callers that don't (closeAndRemoveProxy, removeProxy) rely
// on proxyMu alone, which is sufficient because removal is keyed by
// hostname and no concurrent event can produce the same hostname without
// hostLocks coordination.
//
// Returns the removed proxy, or nil if no match was found.
func (pm *ProxyManager) removeAndTeardown(hostname string, identity func(*Proxy) bool) *Proxy {
	pm.proxyMu.Lock()
	proxy, exists := pm.Proxies[hostname]
	if !exists || (identity != nil && !identity(proxy)) {
		pm.proxyMu.Unlock()
		return nil
	}

	delete(pm.Proxies, hostname)
	if pm.targetIndex[proxy.Config.TargetID] == hostname {
		delete(pm.targetIndex, proxy.Config.TargetID)
	}
	pm.proxyMu.Unlock()

	pm.teardownProxy(proxy)
	return proxy
}

// closeAndRemoveProxy closes and removes any proxy with the given hostname.
// If newProxyProvider is provided and differs from the old proxy's provider,
// a warning is logged since the old Tailscale machine may remain in the tailnet.
func (pm *ProxyManager) closeAndRemoveProxy(hostname string, newProxyProvider ...string) {
	removed := pm.removeAndTeardown(hostname, nil)
	if removed != nil {
		if len(newProxyProvider) > 0 && removed.Config.ProxyProvider != newProxyProvider[0] {
			pm.log.Warn().
				Str("proxy", hostname).
				Str("old_provider", removed.Config.ProxyProvider).
				Str("new_provider", newProxyProvider[0]).
				Msg("Proxy provider changed — the old Tailscale machine may need manual cleanup in the admin console")
		}
		pm.log.Debug().Str("proxy", hostname).Msg("Closed existing proxy for replacement")
	}
}

// closeProxyIfStillCurrent tears down target only if it is still the proxy
// currently registered for its hostname. Prevents a concurrent replacement
// (e.g. a start event for a different target ID that mapped to the same
// hostname) from being destroyed by stale failure cleanup.
//
// Acquires hostLocks for the hostname BEFORE the identity check and holds it
// across teardown. This closes the TOCTOU window that existed when the check
// was done under RLock and the delete under a separate Lock: a concurrent
// newProxy (which also acquires hostLocks) cannot interleave between check
// and teardown.
func (pm *ProxyManager) closeProxyIfStillCurrent(target *Proxy) {
	hostname := target.Config.Hostname

	pm.hostLocks.Lock(hostname)
	defer pm.hostLocks.Unlock(hostname)

	if pm.removeAndTeardown(hostname, func(p *Proxy) bool { return p == target }) == nil {
		pm.log.Debug().
			Str("proxy", hostname).
			Msg("proxy was replaced before failure cleanup — skipping teardown")
	}
}

// removeProxy removes a Proxy from the ProxyManager.
func (pm *ProxyManager) removeProxy(hostname string) {
	if removed := pm.removeAndTeardown(hostname, nil); removed != nil {
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
		pm.closeProxyIfStillCurrent(p)
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
	pm.proxyMu.Lock()
	defer pm.proxyMu.Unlock()

	if pm.stopping.Load() {
		return errors.New("proxy manager is shutting down")
	}

	if proxyConfig.Domain != "" {
		p.setupWg.Add(1)
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
