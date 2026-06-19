// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package proxymanager

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/rs/zerolog"
	"go.opentelemetry.io/otel/trace"

	"github.com/almeidapaulopt/tsdproxy/internal/core/metrics"
	"github.com/almeidapaulopt/tsdproxy/internal/dnsproviders"
	"github.com/almeidapaulopt/tsdproxy/internal/model"
	"github.com/almeidapaulopt/tsdproxy/internal/proxyproviders"
	"github.com/almeidapaulopt/tsdproxy/internal/tlsproviders"
)

const maxStatusHistory = 5

type (
	StatusTransition struct {
		Timestamp time.Time
		Status    model.ProxyStatus
	}

	// Proxy holds the state for a single proxy.
	//
	// Lock hierarchy (acquire in this order to prevent deadlocks):
	//
	//   1. opMu  — coarse-grained operation mutex.
	//      Serializes lifecycle operations (Start, Close, Pause, Resume)
	//      so that at most one lifecycle transition is in-flight at a time.
	//      Held for the entire duration of Start() and Close(), including
	//      blocking calls to the provider proxy.
	//
	//   2. mtx   — fine-grained state read-write mutex.
	//      Guards all mutable state fields listed below. Readers acquire
	//      RLock; writers acquire Lock. Always acquired AFTER opMu when
	//      both are needed (e.g. Close acquires opMu then mtx to reset
	//      paused).
	//
	// mtx guards the following fields:
	//   - tlsProvider, dnsProvider (provider references)
	//   - health, healthPortName (health checker state)
	//   - ports (port handler map)
	//   - status, statusHistory, tlsStatus, dnsStatus (status tracking)
	//   - domainError (domain setup error message)
	//   - paused, metricsReady (boolean flags)
	//   - startedAt (set once in NewProxy, read under RLock)
	//
	// Fields NOT guarded by mtx (immutable after construction or
	// protected by other mechanisms):
	//   - log, tracerProvider, Config, metrics, proxyAuthToken, httpPort
	//   - providerProxy (set once in NewProxy)
	//   - onUpdate (set once in newProxy before map insertion)
	//   - reResolveConfig (set once in newProxy before map insertion)
	//   - ctx, cancel (managed via context package)
	//   - logBuffer (thread-safe LogRingBuffer)
	//   - eventsWg, setupWg (sync.WaitGroup primitives)
	//   - certTrackerStop, certTrackerDone (initialized in
	//     startCertExpiryTracking, read in stopCertTracker; synchronized
	//     via setupWg — teardownProxy calls setupWg.Wait() before
	//     stopCertTracker, guaranteeing the channels are initialized)
	//   - certTrackerStopOnce (zero-value sync.Once, inherently safe)
	Proxy struct {
		log                 zerolog.Logger
		startedAt           time.Time
		tracerProvider      trace.TracerProvider
		ctx                 context.Context
		tlsProvider         tlsproviders.Provider
		dnsProvider         dnsproviders.Provider
		providerProxy       proxyproviders.ProxyInterface
		certTrackerDone     chan struct{}
		onUpdate            func(event model.ProxyEvent)
		cancel              context.CancelFunc
		reResolveConfig     func() (*model.Config, error)
		Config              *model.Config
		metrics             *metrics.Metrics
		ports               map[string]portHandler
		certTrackerStop     chan struct{}
		health              *healthChecker
		logBuffer           *LogRingBuffer
		urlReady            chan struct{}
		proxyAuthToken      string
		healthPortName      string
		lastError           string
		domainError         string
		statusHistory       []StatusTransition
		eventsWg            sync.WaitGroup
		setupWg             sync.WaitGroup
		dnsStatus           dnsproviders.DNSStatus
		tlsStatus           tlsproviders.TLSStatus
		status              model.ProxyStatus
		mtx                 sync.RWMutex
		certTrackerStopOnce sync.Once
		urlOnce             sync.Once
		closeOnce           sync.Once
		opMu                sync.Mutex
		httpPort            uint16
		paused              bool
		metricsReady        bool
	}
)

// ProxyParams holds the parameters for creating a new Proxy.
type ProxyParams struct {
	Ctx            context.Context
	Log            zerolog.Logger
	ProxyProvider  proxyproviders.Provider
	TracerProvider trace.TracerProvider
	Config         *model.Config
	Metrics        *metrics.Metrics
	ProxyAuthToken string
	HTTPPort       uint16
}

func NewProxy(params ProxyParams) (*Proxy, error) {
	var err error

	log := params.Log.With().Str("proxyname", params.Config.Hostname).Logger()
	log.Info().Str("hostname", params.Config.Hostname).Msg("setting up proxy")

	log.Debug().Str("hostname", params.Config.Hostname).
		Msg("initializing proxy")

	pProvider, err := params.ProxyProvider.NewProxy(params.Config)
	if err != nil {
		return nil, fmt.Errorf("error initializing proxy on proxyProvider: %w", err)
	}

	log.Debug().
		Str("hostname", params.Config.Hostname).
		Msg("Proxy server created successfully")

	parentCtx := params.Ctx
	if parentCtx == nil {
		parentCtx = context.Background()
	}
	ctx, cancel := context.WithCancel(parentCtx)

	var logBuffer *LogRingBuffer
	if params.Config.ProxyAccessLog {
		logBuffer = NewLogRingBuffer(log, DefaultLogBufferSize)
	}

	p := &Proxy{
		log:            log,
		Config:         params.Config,
		ctx:            ctx,
		cancel:         cancel,
		providerProxy:  pProvider,
		ports:          make(map[string]portHandler),
		metrics:        params.Metrics,
		statusHistory:  make([]StatusTransition, 0, maxStatusHistory),
		startedAt:      time.Now(),
		logBuffer:      logBuffer,
		tracerProvider: params.TracerProvider,
		httpPort:       params.HTTPPort,
		proxyAuthToken: params.ProxyAuthToken,
		urlReady:       make(chan struct{}),
	}

	p.initPorts()

	return p, nil
}

func (proxy *Proxy) Start() error {
	proxy.opMu.Lock()

	// Phase 1: start the proxy provider (quick — starts tsnet server and
	// the watchStatus goroutine, but does not wait for authentication).
	if err := proxy.startProvider(); err != nil {
		proxy.opMu.Unlock()
		return err
	}

	// Start health checker and event watcher only after the provider
	// has successfully started. This avoids launching goroutines on a
	// proxy whose provider failed to initialize.
	proxy.startHealthChecker()

	proxy.eventsWg.Add(1)
	go func() {
		defer proxy.eventsWg.Done()
		eventsCh := proxy.providerProxy.WatchEvents()
		for {
			select {
			case event, ok := <-eventsCh:
				if !ok {
					return
				}
				proxy.mtx.Lock()
				proxy.lastError = event.ErrorMessage
				proxy.mtx.Unlock()
				proxy.setStatus(event.Status)
				proxy.notifyURLReady()
			case <-proxy.ctx.Done():
				return
			}
		}
	}()

	proxy.opMu.Unlock()

	// Phase 2: create listeners. This may block when interactive Tailscale
	// login is required — tsnet.ListenTLS calls s.Up() which waits for the
	// node to reach Running state. opMu is released so Close() can proceed
	// concurrently (e.g. container stops while auth is still pending).
	return proxy.startListeners()
}

// Close initiates the proxy shutdown procedure. Safe to call multiple times
// from concurrent goroutines — closeOnce guarantees the teardown sequence
// runs exactly once. Subsequent calls return without side effects.
func (proxy *Proxy) Close() {
	proxy.closeOnce.Do(func() {
		proxy.closeInternal()
	})
}

func (proxy *Proxy) closeInternal() {
	proxy.opMu.Lock()
	defer proxy.opMu.Unlock()

	proxy.mtx.Lock()
	proxy.paused = false
	proxy.mtx.Unlock()

	proxy.setStatus(model.ProxyStatusStopping)

	proxy.stopHealthChecker()

	proxy.cancel()

	if proxy.logBuffer != nil {
		proxy.logBuffer.Close()
	}

	proxy.close()

	proxy.eventsWg.Wait()

	proxy.setStatus(model.ProxyStatusStopped)
}

// cancelCtx cancels the proxy's context without closing listeners.
// Use to unblock setup goroutines before waiting for them with setupWg.Wait().
func (proxy *Proxy) cancelCtx() {
	proxy.cancel()
}

func (proxy *Proxy) notifyURLReady() {
	if proxy.urlReady == nil {
		return
	}
	if url := proxy.providerProxy.GetURL(); url != "" {
		proxy.urlOnce.Do(func() {
			close(proxy.urlReady)
		})
	}
}

// Pause stops all port listeners and health checks while keeping the
// provider proxy (tsnet.Server) alive. The proxy status is set to Paused.
func (proxy *Proxy) Pause() error {
	proxy.opMu.Lock()
	defer proxy.opMu.Unlock()

	proxy.mtx.Lock()
	if proxy.paused {
		proxy.mtx.Unlock()
		return fmt.Errorf("proxy %s is already paused", proxy.Config.Hostname)
	}
	if proxy.status != model.ProxyStatusRunning {
		proxy.mtx.Unlock()
		return fmt.Errorf("cannot pause proxy %s: current status is %s, expected Running",
			proxy.Config.Hostname, proxy.status.String())
	}
	proxy.paused = true
	proxy.mtx.Unlock()

	proxy.stopHealthChecker()
	proxy.closePorts()
	proxy.resetRuntimeMetrics()
	proxy.setStatus(model.ProxyStatusPaused)

	proxy.log.Info().Msg("proxy paused")
	return nil
}

// resetRuntimeMetrics zeroes the per-port and health gauges that no longer
// reflect reality once the proxy is paused. Without this, Prometheus would
// keep reporting the pre-pause "healthy / N active connections" values
// for the lifetime of the pause. ProxyStatus is left untouched.
func (proxy *Proxy) resetRuntimeMetrics() {
	if proxy.metrics == nil {
		return
	}
	m := proxy.metrics
	hostname := proxy.Config.Hostname

	m.SetProxyUp(hostname, -1)
	m.ResetProxyPortMetrics(hostname)
}

// Resume re-initializes port handlers and restarts listeners after a Pause.
func (proxy *Proxy) Resume() error {
	proxy.opMu.Lock()
	defer proxy.opMu.Unlock()

	proxy.mtx.Lock()
	if !proxy.paused {
		proxy.mtx.Unlock()
		return fmt.Errorf("proxy %s is not paused", proxy.Config.Hostname)
	}
	proxy.paused = false
	proxy.mtx.Unlock()

	proxy.initPorts()

	proxy.mtx.RLock()
	portsConfig := proxy.Config.Ports
	proxy.mtx.RUnlock()

	var listenerErrors int
	for k, pc := range portsConfig {
		if pc.ProxyProtocol == model.ProtoUDP {
			packetConn, err := proxy.providerProxy.GetPacketConn(k)
			if err != nil {
				proxy.log.Error().Err(err).Str("port", k).Msg("error getting UDP packet conn for resume")
				listenerErrors++
				continue
			}
			proxy.startPacketPort(k, packetConn)
		} else {
			l, err := proxy.getListenerForPort(k, pc)
			if err != nil {
				proxy.log.Error().Err(err).Str("port", k).Msg("error getting listener for resume")
				listenerErrors++
				continue
			}
			proxy.startPort(k, l)
		}
	}

	if listenerErrors > 0 && listenerErrors == len(portsConfig) {
		// Re-pause so the proxy is not left in a zombie state
		// (paused=false + no listeners + no health checker). Without this,
		// the operator must manually Restart from the dashboard.
		proxy.mtx.Lock()
		proxy.paused = true
		proxy.mtx.Unlock()

		proxy.setStatus(model.ProxyStatusError)
		proxy.log.Error().Msg("proxy resume failed: all listeners errored, re-pausing")
		return fmt.Errorf("proxy %s resume failed: all %d listeners errored", proxy.Config.Hostname, listenerErrors)
	}

	proxy.startHealthChecker()
	proxy.setStatus(model.ProxyStatusRunning)

	if listenerErrors > 0 {
		proxy.log.Warn().Int("failed", listenerErrors).Int("total", len(portsConfig)).Msg("proxy resumed with some listener errors")
	} else {
		proxy.log.Info().Msg("proxy resumed")
	}
	return nil
}

func (proxy *Proxy) setMetricsReady(ready bool) {
	proxy.mtx.Lock()
	proxy.metricsReady = ready
	currentStatus := proxy.status
	m := proxy.metrics
	proxy.mtx.Unlock()

	if ready && m != nil {
		m.SetProxyStatus(proxy.Config.Hostname, currentStatus.String())
	}
}

// closeAndClearPorts extracts all port handlers under the write lock, clears
// the ports map, then closes them. Used by Pause() so Resume() can rebuild
// the map from scratch.
func (proxy *Proxy) closeAndClearPorts() error {
	handlers := make([]portHandler, 0, len(proxy.ports))
	proxy.mtx.Lock()
	for k, p := range proxy.ports {
		handlers = append(handlers, p)
		delete(proxy.ports, k)
	}
	proxy.mtx.Unlock()
	return closePortHandlers(handlers)
}

// closePortsKeepMap closes all port handlers under the read lock but leaves
// the ports map intact. Used by Close() where the map entries are never read
// again after return.
func (proxy *Proxy) closePortsKeepMap() error {
	proxy.mtx.RLock()
	handlers := make([]portHandler, 0, len(proxy.ports))
	for _, p := range proxy.ports {
		handlers = append(handlers, p)
	}
	proxy.mtx.RUnlock()
	return closePortHandlers(handlers)
}

func closePortHandlers(handlers []portHandler) error {
	var errs error
	for _, p := range handlers {
		errs = errors.Join(errs, p.close())
	}
	return errs
}

// closePorts closes all port handlers without closing the providerProxy.
func (proxy *Proxy) closePorts() {
	errs := proxy.closeAndClearPorts()

	if errs != nil && !errors.Is(errs, context.Canceled) && !errors.Is(errs, net.ErrClosed) {
		proxy.log.Error().Err(errs).Msg("error closing port handlers")
	}

	proxy.log.Info().Str("name", proxy.Config.Hostname).Msg("port handlers closed")
}

func (proxy *Proxy) GetStatus() model.ProxyStatus {
	proxy.mtx.RLock()
	defer proxy.mtx.RUnlock()

	return proxy.status
}

func (proxy *Proxy) GetURL() string {
	proxy.mtx.RLock()
	domain := proxy.Config.Domain
	tlsOk := proxy.tlsStatus == tlsproviders.TLSStatusActive
	proxy.mtx.RUnlock()

	if domain != "" && tlsOk {
		return "https://" + domain
	}
	return proxy.providerProxy.GetURL()
}

func (proxy *Proxy) GetAuthURL() string {
	return proxy.providerProxy.GetAuthURL()
}

func (proxy *Proxy) GetDNSStatus() dnsproviders.DNSStatus {
	proxy.mtx.RLock()
	defer proxy.mtx.RUnlock()
	return proxy.dnsStatus
}

func (proxy *Proxy) GetTLSStatus() tlsproviders.TLSStatus {
	proxy.mtx.RLock()
	defer proxy.mtx.RUnlock()
	return proxy.tlsStatus
}

// SetDomainError stores a domain setup error so the dashboard can
// indicate the proxy is running in a degraded state (without custom domain).
func (proxy *Proxy) SetDomainError(msg string) {
	proxy.mtx.Lock()
	proxy.domainError = msg
	proxy.mtx.Unlock()
}

// GetDomainError returns the domain setup error, if any.
func (proxy *Proxy) GetDomainError() string {
	proxy.mtx.RLock()
	defer proxy.mtx.RUnlock()
	return proxy.domainError
}

func (proxy *Proxy) GetLastError() string {
	proxy.mtx.RLock()
	defer proxy.mtx.RUnlock()
	return proxy.lastError
}

// SetDNSAndTLSProviders attaches the resolved DNS and TLS providers to the proxy.
// Must be called at most once per Proxy lifecycle (during initial setup in
// newProxy). Calling it again would silently overwrite the old providers without
// closing them, leaking any background resources (e.g. certmagic cache goroutine).
func (proxy *Proxy) SetDNSAndTLSProviders(dns dnsproviders.Provider, tls tlsproviders.Provider) {
	proxy.mtx.Lock()
	defer proxy.mtx.Unlock()
	proxy.dnsProvider = dns
	proxy.tlsProvider = tls
}

func (proxy *Proxy) setDNSStatus(status dnsproviders.DNSStatus) {
	proxy.mtx.Lock()
	proxy.dnsStatus = status
	proxy.mtx.Unlock()
}

func (proxy *Proxy) setTLSStatus(status tlsproviders.TLSStatus) {
	proxy.mtx.Lock()
	proxy.tlsStatus = status
	proxy.mtx.Unlock()
}

func (proxy *Proxy) GetHealth() HealthResult {
	proxy.mtx.RLock()
	hc := proxy.health
	proxy.mtx.RUnlock()
	if hc == nil {
		return HealthResult{Status: HealthUnknown}
	}
	return hc.GetHealth()
}

func (proxy *Proxy) GetStatusHistory() []StatusTransition {
	proxy.mtx.RLock()
	defer proxy.mtx.RUnlock()

	history := make([]StatusTransition, len(proxy.statusHistory))
	copy(history, proxy.statusHistory)
	return history
}

func (proxy *Proxy) GetUptime() time.Duration {
	proxy.mtx.RLock()
	defer proxy.mtx.RUnlock()

	if proxy.startedAt.IsZero() {
		return 0
	}
	return time.Since(proxy.startedAt)
}

// SubscribeLogs returns a snapshot of existing log lines and a channel for
// live updates. Returns nil slice and nil channel if access logging is disabled.
func (proxy *Proxy) SubscribeLogs() (snapshot []string, ch chan string) {
	if proxy.logBuffer == nil {
		return nil, nil
	}
	return proxy.logBuffer.SubscribeWithSnapshot()
}

// UnsubscribeLogs removes a log subscription. Safe to call with nil channel
// or when access logging is disabled.
func (proxy *Proxy) UnsubscribeLogs(ch chan string) {
	if ch == nil || proxy.logBuffer == nil {
		return
	}
	proxy.logBuffer.Unsubscribe(ch)
}

func (proxy *Proxy) ProviderUserMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		who := proxy.providerProxy.Whois(r)

		ctx := model.WhoisNewContext(r.Context(), who)

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (proxy *Proxy) close() {
	proxy.log.Info().Str("name", proxy.Config.Hostname).Msg("stopping proxy")

	errs := proxy.closePortsKeepMap()

	if proxy.providerProxy != nil {
		errs = errors.Join(errs, proxy.providerProxy.Close())
	}

	if errs != nil && !errors.Is(errs, context.Canceled) && !errors.Is(errs, net.ErrClosed) {
		proxy.log.Error().Err(errs).Msg("Error stopping proxy")
	}

	proxy.log.Info().Str("name", proxy.Config.Hostname).Msg("proxy stopped")
}

func (proxy *Proxy) setStatus(status model.ProxyStatus) {
	proxy.mtx.Lock()

	if proxy.status == status {
		proxy.mtx.Unlock()
		return
	}

	// When paused, block status updates from provider events.
	// Only internal transitions (Pause → Paused, Resume → Running) are allowed.
	if proxy.paused && status != model.ProxyStatusPaused {
		proxy.mtx.Unlock()
		return
	}

	oldStatus := proxy.status
	proxy.status = status

	proxy.statusHistory = append(proxy.statusHistory, StatusTransition{
		Status:    status,
		Timestamp: time.Now(),
	})
	if len(proxy.statusHistory) > maxStatusHistory {
		proxy.statusHistory = proxy.statusHistory[len(proxy.statusHistory)-maxStatusHistory:]
	}

	hostname := proxy.Config.Hostname
	m := proxy.metrics
	ready := proxy.metricsReady
	lastErr := proxy.lastError

	proxy.mtx.Unlock()

	if m != nil && ready {
		m.SetProxyStatus(hostname, status.String())
	}

	if proxy.onUpdate != nil {
		proxy.onUpdate(model.ProxyEvent{
			ID:           hostname,
			Status:       status,
			OldStatus:    oldStatus,
			ErrorMessage: lastErr,
		})
	}
}
