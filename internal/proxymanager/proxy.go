// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package proxymanager

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"sort"
	"sync"
	"time"

	"github.com/almeidapaulopt/tsdproxy/internal/core/metrics"
	"github.com/almeidapaulopt/tsdproxy/internal/model"
	"github.com/almeidapaulopt/tsdproxy/internal/proxyproviders"

	"github.com/rs/zerolog"
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

	// Proxy struct is a struct that contains all the information needed to run a proxy.
	Proxy struct {
		log             zerolog.Logger
		startedAt       time.Time
		providerProxy   proxyproviders.ProxyInterface
		ctx             context.Context
		ports           map[string]portHandler
		health          *healthChecker
		URL             *url.URL
		cancel          context.CancelFunc
		onUpdate        func(event model.ProxyEvent)
		logBuffer       *LogRingBuffer
		reResolveConfig func() (*model.Config, error)
		Config          *model.Config
		metrics         *metrics.Metrics
		healthPortName  string
		statusHistory   []StatusTransition
		status          model.ProxyStatus
		mtx             sync.RWMutex
		opMu            sync.Mutex
		paused          bool
	}
)

// NewProxy function is a function that creates a new proxy.
func NewProxy(log zerolog.Logger,
	pcfg *model.Config,
	proxyProvider proxyproviders.Provider,
	m *metrics.Metrics,
) (*Proxy, error) {
	//
	var err error

	log = log.With().Str("proxyname", pcfg.Hostname).Logger()
	log.Info().Str("hostname", pcfg.Hostname).Msg("setting up proxy")

	log.Debug().Str("hostname", pcfg.Hostname).
		Msg("initializing proxy")

	// Create the proxyProvider proxy
	//
	pProvider, err := proxyProvider.NewProxy(pcfg)
	if err != nil {
		return nil, fmt.Errorf("error initializing proxy on proxyProvider: %w", err)
	}

	log.Debug().
		Str("hostname", pcfg.Hostname).
		Msg("Proxy server created successfully")

	ctx, cancel := context.WithCancel(context.Background())

	var logBuffer *LogRingBuffer
	if pcfg.ProxyAccessLog {
		logBuffer = NewLogRingBuffer(log, DefaultLogBufferSize)
	}

	p := &Proxy{
		log:           log,
		Config:        pcfg,
		ctx:           ctx,
		cancel:        cancel,
		providerProxy: pProvider,
		ports:         make(map[string]portHandler),
		metrics:       m,
		statusHistory: make([]StatusTransition, 0, maxStatusHistory),
		startedAt:     time.Now(),
		logBuffer:     logBuffer,
	}

	p.initPorts()

	return p, nil
}

func (proxy *Proxy) Start() error {
	proxy.opMu.Lock()
	defer proxy.opMu.Unlock()

	proxy.startHealthChecker()

	go func() {
		for event := range proxy.providerProxy.WatchEvents() {
			proxy.setStatus(event.Status)
		}
	}()

	return proxy.start()
}

// Close method is a method that initiate proxy close procedure.
func (proxy *Proxy) Close() {
	proxy.opMu.Lock()
	defer proxy.opMu.Unlock()

	proxy.setStatus(model.ProxyStatusStopping)

	proxy.stopHealthChecker()

	// cancel context
	proxy.cancel()

	// Close log subscribers so SSE handlers unblock.
	if proxy.logBuffer != nil {
		proxy.logBuffer.Close()
	}

	// make sure all listeners are closed
	proxy.close()

	proxy.setStatus(model.ProxyStatusStopped)
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

	// Re-init ports from config
	proxy.initPorts()

	// Start each port with a new listener from the provider
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
			l, err := proxy.providerProxy.GetListener(k)
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

// closePorts closes all port handlers without closing the providerProxy.
func (proxy *Proxy) closePorts() {
	var errs error

	proxy.mtx.Lock()
	for k, p := range proxy.ports {
		errs = errors.Join(errs, p.close())
		delete(proxy.ports, k)
	}
	proxy.mtx.Unlock()

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
	return proxy.providerProxy.GetURL()
}

func (proxy *Proxy) GetAuthURL() string {
	return proxy.providerProxy.GetAuthURL()
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

// Start method is a method that starts the proxy.
func (proxy *Proxy) start() error {
	proxy.log.Info().Msg("starting proxy")

	proxy.mtx.RLock()
	portsConfig := proxy.Config.Ports
	portsCount := len(proxy.ports)
	proxy.mtx.RUnlock()

	if portsCount == 0 {
		return errors.New("no ports configured")
	}

	if err := proxy.providerProxy.Start(proxy.ctx); err != nil {
		return fmt.Errorf("error starting with proxy provider: %w", err)
	}

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
			l, err := proxy.providerProxy.GetListener(k)
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

	// make sure port exists
	if p, ok := proxy.ports[name]; ok {
		go func() {
			if err := p.startWithListener(l); err != nil {
				proxy.log.Error().Err(err).Msg("error starting port")
				proxy.setStatus(model.ProxyStatusError)
			}
		}()
	}
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

// close method is a method that closes all listeners ans httpServer.
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

	proxy.mtx.Unlock()

	if proxy.onUpdate != nil {
		proxy.onUpdate(model.ProxyEvent{
			ID:        proxy.Config.Hostname,
			Status:    status,
			OldStatus: oldStatus,
		})
	}
}
