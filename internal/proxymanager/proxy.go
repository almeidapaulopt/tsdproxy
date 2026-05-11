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
	"sync"
	"time"

	"github.com/almeidapaulopt/tsdproxy/internal/core/metrics"
	"github.com/almeidapaulopt/tsdproxy/internal/model"
	"github.com/almeidapaulopt/tsdproxy/internal/proxyproviders"

	"github.com/rs/zerolog"
)

const maxStatusHistory = 5

type (
	StatusTransition struct {
		Status    model.ProxyStatus
		Timestamp time.Time
	}

	// Proxy struct is a struct that contains all the information needed to run a proxy.
	Proxy struct {
		onUpdate func(event model.ProxyEvent)

		log           zerolog.Logger
		ctx           context.Context
		providerProxy proxyproviders.ProxyInterface
		Config        *model.Config
		URL           *url.URL
		cancel        context.CancelFunc
		ports         map[string]portHandler
		mtx           sync.RWMutex
		status        model.ProxyStatus
		metrics       *metrics.Metrics
		health        *healthChecker
		statusHistory []StatusTransition
		startedAt     time.Time
		logBuffer     *LogRingBuffer
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

func (proxy *Proxy) Start() {
	go proxy.start()

	proxy.startHealthChecker()

	go func() {
		for event := range proxy.providerProxy.WatchEvents() {
			proxy.setStatus(event.Status)
		}
	}()
}

// Close method is a method that initiate proxy close procedure.
func (proxy *Proxy) Close() {
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
	return proxy.health.GetHealth()
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
	for _, pc := range proxy.Config.Ports {
		if pc.IsRedirect {
			continue
		}
		target := pc.GetFirstTarget()
		if target == nil || target.Host == "" {
			continue
		}

		scheme := pc.ProxyProtocol
		var checkTarget string
		if scheme == "http" || scheme == "https" {
			checkTarget = target.String()
		} else {
			checkTarget = target.Host
		}

		proxy.health = newHealthChecker(proxy.log, checkTarget, scheme)
		proxy.health.start()
		return
	}
}

func (proxy *Proxy) stopHealthChecker() {
	if proxy.health != nil {
		proxy.health.stop()
	}
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
		} else if v.ProxyProtocol == "http" || v.ProxyProtocol == "https" {
			ph = newPortProxy(proxy.ctx, v, log, proxy.Config.ProxyAccessLog, proxy.ProviderUserMiddleware, proxy.metrics, proxy.Config.Hostname, k, proxy.logBuffer)
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
func (proxy *Proxy) start() {
	proxy.log.Info().Msg("starting proxy")

	proxy.mtx.RLock()
	portsConfig := proxy.Config.Ports
	portsCount := len(proxy.ports)
	proxy.mtx.RUnlock()

	if portsCount == 0 {
		proxy.log.Warn().Msg("No ports configured")

		// Release context and provider resources (e.g. tsnet node) without
		// transitioning to Stopped, so the dashboard keeps showing the Error
		// state for this misconfigured proxy.
		proxy.cancel()
		proxy.close()
		proxy.setStatus(model.ProxyStatusError)

		return
	}

	if err := proxy.providerProxy.Start(proxy.ctx); err != nil {
		proxy.log.Error().Err(err).Msg("Error starting with proxy provider")
		proxy.Close()
		return
	}

	var l net.Listener
	var err error

	for k := range portsConfig {
		proxy.log.Debug().Str("port", k).Msg("Starting proxy port")

		l, err = proxy.providerProxy.GetListener(k)
		if err != nil {
			proxy.log.Error().Err(err).Str("port", k).Msg("Error adding listener")
			continue
		}

		proxy.startPort(k, l)
	}
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

// close method is a method that closes all listeners ans httpServer.
func (proxy *Proxy) close() {
	var errs error
	proxy.log.Info().Str("name", proxy.Config.Hostname).Msg("stopping proxy")

	for _, p := range proxy.ports {
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
			ID:     proxy.Config.Hostname,
			Status: status,
		})
	}
}
