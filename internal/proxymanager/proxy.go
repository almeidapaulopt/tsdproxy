// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package proxymanager

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog"
	"go.opentelemetry.io/otel/trace"
	"tailscale.com/client/local"

	"github.com/almeidapaulopt/tsdproxy/internal/core/metrics"
	"github.com/almeidapaulopt/tsdproxy/internal/dnsproviders"
	"github.com/almeidapaulopt/tsdproxy/internal/model"
	"github.com/almeidapaulopt/tsdproxy/internal/proxyproviders"
	tsproxy "github.com/almeidapaulopt/tsdproxy/internal/proxyproviders/tailscale"
	"github.com/almeidapaulopt/tsdproxy/internal/tlsproviders"
)

const maxStatusHistory = 5

// clampDuration converts seconds to time.Duration, clamping to [min, max].
// Prevents time.NewTicker panic from negative durations caused by int64 overflow.
func clampDuration(seconds int, minVal, maxVal time.Duration) time.Duration {
	d := time.Duration(seconds) * time.Second
	if d < minVal {
		return minVal
	}
	if d > maxVal {
		return maxVal
	}
	return d
}

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
	Proxy struct {
		log             zerolog.Logger
		startedAt       time.Time
		tracerProvider  trace.TracerProvider
		ctx             context.Context
		tlsProvider     tlsproviders.Provider
		dnsProvider     dnsproviders.Provider
		providerProxy   proxyproviders.ProxyInterface
		logBuffer       *LogRingBuffer
		urlReady        chan struct{}
		cancel          context.CancelFunc
		reResolveConfig func() (*model.Config, error)
		Config          *model.Config
		metrics         *metrics.Metrics
		ports           map[string]portHandler
		onUpdate        func(event model.ProxyEvent)
		health          *healthChecker
		domainError     string
		proxyAuthToken  string
		healthPortName  string
		statusHistory   []StatusTransition
		setupWg         sync.WaitGroup
		eventsWg        sync.WaitGroup
		tlsStatus       tlsproviders.TLSStatus
		dnsStatus       dnsproviders.DNSStatus
		status          model.ProxyStatus
		mtx             sync.RWMutex
		urlOnce         sync.Once
		opMu            sync.Mutex
		httpPort        uint16
		paused          bool
		metricsReady    bool
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

// Close initiates the proxy shutdown procedure.
func (proxy *Proxy) Close() {
	proxy.opMu.Lock()
	defer proxy.opMu.Unlock()

	proxy.mtx.Lock()
	proxy.paused = false
	proxy.mtx.Unlock()

	proxy.setStatus(model.ProxyStatusStopping)

	proxy.stopHealthChecker()

	proxy.cancel()

	// Close log subscribers so SSE handlers unblock.
	if proxy.logBuffer != nil {
		proxy.logBuffer.Close()
	}

	proxy.close()

	// Wait for the event-watching goroutine to exit so no stale
	// status updates fire after Close() returns.
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
	proxy.setStatus(model.ProxyStatusPaused)

	proxy.log.Info().Msg("proxy paused")
	return nil
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
		proxy.setStatus(model.ProxyStatusError)
		proxy.log.Error().Msg("proxy resume failed: all listeners errored")
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

// closePorts closes all port handlers without closing the providerProxy.
func (proxy *Proxy) closePorts() {
	var errs error

	proxy.mtx.Lock()
	handlers := make([]portHandler, 0, len(proxy.ports))
	for k, p := range proxy.ports {
		handlers = append(handlers, p)
		delete(proxy.ports, k)
	}
	proxy.mtx.Unlock()

	for _, p := range handlers {
		errs = errors.Join(errs, p.close())
	}

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

func (proxy *Proxy) startHealthChecker() {
	if !proxy.Config.HealthCheckEnabled {
		return
	}

	// NOTE: Only the first non-redirect port (sorted by name) gets a health checker.
	// If the proxy has multiple ports, only the first one is monitored.
	keys := make([]string, 0, len(proxy.Config.Ports))
	for k := range proxy.Config.Ports {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		pc := proxy.Config.Ports[k]
		if pc.IsRedirect {
			continue
		}
		target := pc.GetFirstTarget()
		if target == nil || target.Host == "" {
			continue
		}

		scheme := pc.ProxyProtocol
		var checkTarget string
		if scheme == model.ProtoHTTP || scheme == model.ProtoHTTPS {
			checkTarget = target.String()
		} else {
			checkTarget = target.Host
		}

		// Clamp health check durations to safe ranges to prevent
		// time.Duration overflow when converting from int seconds.
		interval := clampDuration(proxy.Config.HealthCheckInterval, time.Second, healthCheckMaxInterval)
		cooldown := clampDuration(proxy.Config.HealthCheckCooldown, 0, healthCheckMaxCooldown)

		hc := newHealthChecker(proxy.log, checkTarget, scheme, interval, proxy.Config.HealthCheckFailures, cooldown, pc.TLSValidate, func() error {
			return proxy.reResolveHealthTarget()
		})

		proxy.mtx.Lock()
		proxy.healthPortName = k
		proxy.health = hc
		proxy.mtx.Unlock()

		hc.start()
		return
	}
}

func (proxy *Proxy) stopHealthChecker() {
	proxy.mtx.RLock()
	hc := proxy.health
	proxy.mtx.RUnlock()
	if hc != nil {
		// stop() blocks until the health check goroutine has exited,
		// ensuring no in-flight checks access proxy state after return.
		hc.stop()
	}
}

func (proxy *Proxy) reResolveHealthTarget() error {
	if !proxy.Config.AutoRestart {
		return nil
	}

	if proxy.reResolveConfig == nil {
		return nil
	}

	newCfg, err := proxy.reResolveConfig()
	if err != nil {
		return fmt.Errorf("re-resolution failed: %w", err)
	}

	if proxy.ctx.Err() != nil {
		return nil
	}

	// RLock protects iterating proxy.Config.Ports map (read-only after construction).
	// Actual target mutation uses targetState.mtx internally, and proxy.health uses atomic operations.
	// The lock also ensures we don't race with startHealthChecker which writes under proxy.mtx.Lock().
	proxy.mtx.RLock()
	defer proxy.mtx.RUnlock()

	for portName, newPC := range newCfg.Ports {
		if newPC.IsRedirect {
			continue
		}

		oldPC, ok := proxy.Config.Ports[portName]
		if !ok {
			continue
		}

		oldTarget := oldPC.GetFirstTarget()
		newTarget := newPC.GetFirstTarget()

		if oldTarget == nil || newTarget == nil {
			continue
		}

		if oldTarget.String() == newTarget.String() {
			continue
		}

		proxy.log.Info().
			Str("port", portName).
			Str("old_target", oldTarget.String()).
			Str("new_target", newTarget.String()).
			Msg("health re-resolution: target changed, hot-swapping")

		oldPC.ReplaceTarget(oldTarget, newTarget)

		if portName == proxy.healthPortName && proxy.health != nil {
			scheme := oldPC.ProxyProtocol
			var checkTarget string
			if scheme == model.ProtoHTTP || scheme == model.ProtoHTTPS {
				checkTarget = newTarget.String()
			} else {
				checkTarget = newTarget.Host
			}
			proxy.health.SetTarget(checkTarget)
		}
	}

	return nil
}

func (proxy *Proxy) ProviderUserMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		who := proxy.providerProxy.Whois(r)

		ctx := model.WhoisNewContext(r.Context(), who)

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (proxy *Proxy) initPorts() {
	for k, v := range proxy.Config.Ports {
		log := proxy.log.With().Str("port", k).Logger()

		var ph portHandler
		if v.IsRedirect {
			ph = newPortRedirect(proxy.ctx, v, log)
		} else if v.ProxyProtocol == model.ProtoHTTP || v.ProxyProtocol == model.ProtoHTTPS {
			ph = newPortProxy(
				proxy.ctx, v, log,
				proxy.Config.ProxyAccessLog,
				proxy.ProviderUserMiddleware,
				proxy.metrics,
				proxy.Config.Hostname,
				k, proxy.logBuffer,
				proxy.Config.IdentityHeaders,
				proxy.tracerProvider,
				proxy.httpPort,
				proxy.Config.RateLimitEnabled,
				proxy.Config.RateLimitRPS,
				proxy.Config.RateLimitBurst,
				proxy.proxyAuthToken,
			)
		} else if v.ProxyProtocol == model.ProtoUDP {
			ph = newPortUDP(proxy.ctx, v, log)
		} else {
			ph = newPortTCP(proxy.ctx, v, log)
		}

		proxy.log.Debug().Any("port", ph).Msg("newport")

		proxy.mtx.Lock()
		proxy.ports[k] = ph
		proxy.mtx.Unlock()
	}
}

func (proxy *Proxy) startProvider() error {
	proxy.log.Info().Msg("starting proxy")

	proxy.mtx.RLock()
	portsCount := len(proxy.ports)
	proxy.mtx.RUnlock()

	if portsCount == 0 {
		return errors.New("no ports configured")
	}

	if err := proxy.providerProxy.Start(proxy.ctx); err != nil {
		return fmt.Errorf("error starting with proxy provider: %w", err)
	}

	return nil
}

func (proxy *Proxy) startListeners() error {
	proxy.mtx.RLock()
	portsConfig := proxy.Config.Ports
	proxy.mtx.RUnlock()

	var listenerErrors int
	for k, pc := range portsConfig {
		proxy.log.Debug().Str("port", k).Msg("Starting proxy port")

		if pc.ProxyProtocol == model.ProtoUDP {
			packetConn, err := proxy.providerProxy.GetPacketConn(k)
			if err != nil {
				proxy.log.Error().Err(err).Str("port", k).Msg("Error getting UDP packet conn")
				listenerErrors++
				continue
			}
			proxy.startPacketPort(k, packetConn)
		} else {
			l, err := proxy.getListenerForPort(k, pc)
			if err != nil {
				proxy.log.Error().Err(err).Str("port", k).Msg("Error adding listener")
				listenerErrors++
				continue
			}
			proxy.startPort(k, l)
		}
	}

	if listenerErrors > 0 && listenerErrors == len(portsConfig) {
		return fmt.Errorf("all %d listeners failed", listenerErrors)
	}

	if listenerErrors > 0 {
		proxy.log.Warn().Int("failed", listenerErrors).Int("total", len(portsConfig)).Msg("proxy started with some listener errors")
	}

	return nil
}

func (proxy *Proxy) startPort(name string, l net.Listener) {
	proxy.mtx.RLock()
	defer proxy.mtx.RUnlock()

	if p, ok := proxy.ports[name]; ok {
		go func() {
			if err := p.startWithListener(l); err != nil {
				proxy.log.Error().Err(err).Msg("error starting port")
				proxy.setStatus(model.ProxyStatusError)
			}
		}()
	}
}

func (proxy *Proxy) getListenerForPort(portName string, pc model.PortConfig) (net.Listener, error) {
	needsCustomTLS := proxy.Config.Domain != "" &&
		pc.ProxyProtocol == model.ProtoHTTPS &&
		proxy.tlsProvider != nil &&
		proxy.tlsProvider.Name() != model.TLSProviderTailscale

	if needsCustomTLS {
		return proxy.getCustomTLSListener(portName)
	}

	if pc.ProxyProtocol == model.ProtoTCP {
		if raw, ok := proxy.providerProxy.(proxyproviders.RawTCPListener); ok {
			return raw.GetRawTCPListener(portName)
		}
	}

	return proxy.providerProxy.GetListener(portName)
}

func (proxy *Proxy) getCustomTLSListener(portName string) (net.Listener, error) {
	raw, ok := proxy.providerProxy.(proxyproviders.RawTCPListener)
	if !ok {
		return nil, errors.New("custom domain TLS requires raw TCP listener support from proxy provider")
	}

	l, err := raw.GetRawTCPListener(portName)
	if err != nil {
		return nil, fmt.Errorf("get raw tcp listener: %w", err)
	}

	domain := proxy.Config.Domain
	tlsProv := proxy.tlsProvider

	return tls.NewListener(l, &tls.Config{
		MinVersion: tls.VersionTLS12,
		GetCertificate: func(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
			proxy.mtx.RLock()
			tlsActive := proxy.tlsStatus == tlsproviders.TLSStatusActive
			proxy.mtx.RUnlock()

			if tlsActive {
				cert, err := tlsProv.GetCertificate(hello.Context(), domain)
				if err == nil {
					return &cert, nil
				}
				proxy.log.Warn().Err(err).Msg("custom cert lookup failed, falling back to Tailscale cert")
			}

			// Fallback to Tailscale automatic cert
			cert, err := proxy.getTailscaleCertificate(hello.Context())
			if err != nil {
				return nil, fmt.Errorf("no certificate available for %s: %w", domain, err)
			}
			return cert, nil
		},
	}), nil
}

func (proxy *Proxy) getTailscaleCertificate(ctx context.Context) (*tls.Certificate, error) {
	lcGetter, ok := proxy.providerProxy.(interface{ GetLocalClient() *local.Client })
	if !ok {
		return nil, errors.New("tailscale cert not available: provider does not support GetLocalClient")
	}
	lc := lcGetter.GetLocalClient()
	if lc == nil {
		return nil, errors.New("tailscale local client not available")
	}

	rawURL := proxy.providerProxy.GetURL()
	hostname := strings.TrimPrefix(rawURL, "https://")
	hostname = strings.TrimPrefix(hostname, "http://")
	if hostname == "" {
		return nil, errors.New("tailscale hostname not yet available")
	}

	return tsproxy.CertPairToTLSCertificate(ctx, lc, hostname)
}

func (proxy *Proxy) startPacketPort(name string, pc net.PacketConn) {
	proxy.mtx.RLock()
	defer proxy.mtx.RUnlock()

	p, ok := proxy.ports[name]
	if !ok {
		pc.Close()
		return
	}

	udp, ok := p.(*udpPort)
	if !ok {
		pc.Close()
		return
	}

	go func() {
		if err := udp.startWithPacketConn(pc); err != nil {
			proxy.log.Error().Err(err).Msg("error starting UDP port")
			proxy.setStatus(model.ProxyStatusError)
		}
	}()
}

func (proxy *Proxy) close() {
	var errs error
	proxy.log.Info().Str("name", proxy.Config.Hostname).Msg("stopping proxy")

	proxy.mtx.RLock()
	handlers := make([]portHandler, 0, len(proxy.ports))
	for _, p := range proxy.ports {
		handlers = append(handlers, p)
	}
	proxy.mtx.RUnlock()

	for _, p := range handlers {
		errs = errors.Join(errs, p.close())
	}

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

	proxy.mtx.Unlock()

	if m != nil && ready {
		m.SetProxyStatus(hostname, status.String())
	}

	if proxy.onUpdate != nil {
		proxy.onUpdate(model.ProxyEvent{
			ID:        hostname,
			Status:    status,
			OldStatus: oldStatus,
		})
	}
}
