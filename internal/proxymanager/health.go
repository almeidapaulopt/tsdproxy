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
	"sync/atomic"
	"time"

	"github.com/rs/zerolog"

	"github.com/almeidapaulopt/tsdproxy/internal/core/httpclient"
	"github.com/almeidapaulopt/tsdproxy/internal/model"
)

// HealthStatus represents the health of a proxy's backend target.
type HealthStatus int

const (
	HealthUnknown HealthStatus = iota
	HealthHealthy
	HealthDown

	healthCheckMaxInterval = time.Hour
	healthCheckMaxCooldown = 24 * time.Hour
)

func (s HealthStatus) String() string {
	switch s {
	case HealthHealthy:
		return "healthy"
	case HealthDown:
		return "down"
	default:
		return healthStrUnknown
	}
}

const healthStrUnknown = "unknown"

// HealthResult holds the latest health check result for a proxy target.
type HealthResult struct {
	CheckedAt time.Time
	Error     string
	Status    HealthStatus
	Latency   time.Duration
}

type healthChecker struct {
	log                 zerolog.Logger
	cooldownUntil       time.Time
	target              atomic.Value
	ctx                 context.Context
	transport           *http.Transport
	result              atomic.Pointer[HealthResult]
	cancel              context.CancelFunc
	done                chan struct{}
	onRedetect          func() error
	onResult            func(HealthResult)
	httpClient          httpclient.Doer
	scheme              string
	interval            time.Duration
	cooldown            time.Duration
	failThreshold       int
	retryAttempt        int
	consecutiveFailures int
	tlsValidate         bool
}

const (
	healthCheckTimeout = 5 * time.Second
	maxBackoff         = 24 * time.Hour
)

// maxBackoffShift is the maximum bit shift that stays within int64 positive range.
const maxBackoffShift = 62

func nextBackoff(interval time.Duration, attempt int) time.Duration {
	if attempt > maxBackoffShift {
		return maxBackoff
	}
	shift := time.Duration(1 << uint(attempt))
	// Guard against overflow: if interval * shift would exceed maxBackoff, cap it.
	if shift > 0 && interval > maxBackoff/shift {
		return maxBackoff
	}
	d := time.Duration(int64(interval) * int64(shift))
	if d > maxBackoff {
		return maxBackoff
	}
	return d
}

func newHealthChecker(
	log zerolog.Logger, target, scheme string,
	interval time.Duration, failThreshold int,
	cooldown time.Duration, tlsValidate bool,
	onRedetect func() error,
) *healthChecker {
	//
	ctx, cancel := context.WithCancel(context.Background())

	transport := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: !tlsValidate}, //nolint:gosec // G402: config-driven TLS validation toggle
		DialContext:     (&net.Dialer{Timeout: healthCheckTimeout}).DialContext,
	}

	hc := &healthChecker{
		log:    log.With().Str("component", "health").Logger(),
		scheme: scheme,
		ctx:    ctx,
		cancel: cancel,

		interval:      interval,
		failThreshold: failThreshold,
		cooldown:      cooldown,
		tlsValidate:   tlsValidate,
		onRedetect:    onRedetect,
		transport:     transport,
		httpClient: &http.Client{
			Timeout:   healthCheckTimeout,
			Transport: transport,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}

	hc.target.Store(target)
	hc.result.Store(&HealthResult{Status: HealthUnknown})

	return hc
}

// SetTarget atomically updates the target address for health checks.
// Safe to call from any goroutine while checks are running.
func (hc *healthChecker) SetTarget(target string) {
	hc.target.Store(target)
}

func (hc *healthChecker) getTarget() string {
	t, _ := hc.target.Load().(string)
	return t
}

func (hc *healthChecker) start() {
	hc.done = make(chan struct{})
	go hc.run()
}

func (hc *healthChecker) stop() {
	hc.cancel()
	if hc.done != nil {
		<-hc.done
	}
	if hc.transport != nil {
		hc.transport.CloseIdleConnections()
	}
}

func (hc *healthChecker) run() {
	defer close(hc.done)
	hc.check()
	hc.notifyResult()

	ticker := time.NewTicker(hc.interval)
	defer ticker.Stop()

	for {
		select {
		case <-hc.ctx.Done():
			return
		case <-ticker.C:
			hc.check()
			hc.notifyResult()
		}
	}
}

func (hc *healthChecker) notifyResult() {
	if hc.onResult == nil {
		return
	}
	if r := hc.result.Load(); r != nil {
		hc.onResult(*r)
	}
}

func (hc *healthChecker) check() {
	ctx, cancel := context.WithTimeout(hc.ctx, healthCheckTimeout)
	defer cancel()

	var result HealthResult
	result.CheckedAt = time.Now()

	switch hc.scheme {
	case model.ProtoHTTP, model.ProtoHTTPS:
		result = hc.checkHTTP(ctx)
	case model.ProtoUDP:
		result = hc.checkUDP(ctx)
	default:
		result = hc.checkTCP(ctx)
	}

	hc.result.Store(&result)

	hc.log.Debug().
		Str("status", result.Status.String()).
		Dur("latency", result.Latency).
		Msg("health check completed")

	if result.Status == HealthHealthy {
		hc.consecutiveFailures = 0
		hc.retryAttempt = 0
		hc.cooldownUntil = time.Time{}
		return
	}

	if !hc.cooldownUntil.IsZero() && time.Now().Before(hc.cooldownUntil) {
		return
	}

	hc.consecutiveFailures++

	if hc.ctx.Err() == nil && hc.consecutiveFailures >= hc.failThreshold {
		bo := hc.cooldown
		if bo == 0 {
			bo = nextBackoff(hc.interval, hc.retryAttempt)
		}
		hc.retryAttempt++

		hc.log.Warn().
			Int("consecutive_failures", hc.consecutiveFailures).
			Dur("next_retry_after", bo).
			Msg("health check: triggering re-resolution")

		if hc.onRedetect != nil {
			if err := hc.onRedetect(); err != nil {
				hc.log.Error().Err(err).Msg("re-resolution callback failed")
			}
		}

		hc.consecutiveFailures = 0
		hc.cooldownUntil = time.Now().Add(bo)
	}
}

func (hc *healthChecker) checkHTTP(ctx context.Context) HealthResult {
	var result HealthResult
	result.CheckedAt = time.Now()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, hc.getTarget(), nil)
	if err != nil {
		result.Status = HealthDown
		result.Error = fmt.Sprintf("invalid target URL: %v", err)
		return result
	}

	start := time.Now()
	resp, err := hc.httpClient.Do(req)
	result.Latency = time.Since(start)

	if err != nil {
		result.Status = HealthDown
		result.Error = err.Error()
		return result
	}
	defer resp.Body.Close()

	result.Status = HealthHealthy
	return result
}

func (hc *healthChecker) checkTCP(ctx context.Context) HealthResult {
	var result HealthResult
	result.CheckedAt = time.Now()

	var d net.Dialer
	start := time.Now()
	conn, err := d.DialContext(ctx, "tcp", hc.getTarget())
	result.Latency = time.Since(start)

	if err != nil {
		result.Status = HealthDown
		result.Error = err.Error()
		return result
	}
	conn.Close()

	result.Status = HealthHealthy
	return result
}

func (hc *healthChecker) checkUDP(ctx context.Context) HealthResult {
	var result HealthResult
	result.CheckedAt = time.Now()

	addr, err := net.ResolveUDPAddr(model.ProtoUDP, hc.getTarget())
	if err != nil {
		result.Status = HealthDown
		result.Error = fmt.Sprintf("error resolving address: %v", err)
		return result
	}

	conn, err := net.DialUDP(model.ProtoUDP, nil, addr)
	if err != nil {
		result.Status = HealthDown
		result.Error = err.Error()
		return result
	}
	defer conn.Close()

	start := time.Now()
	// Send a probe to trigger ICMP port-unreachable if nothing is listening.
	if _, err := conn.Write([]byte{0}); err != nil {
		result.Latency = time.Since(start)
		result.Status = HealthDown
		result.Error = err.Error()
		return result
	}

	// Wait for either a response, an ICMP error, or context cancellation.
	deadline := time.Now().Add(healthCheckTimeout)
	if err := conn.SetReadDeadline(deadline); err != nil {
		result.Latency = time.Since(start)
		result.Status = HealthDown
		result.Error = err.Error()
		return result
	}

	done := make(chan struct{})
	var readErr error
	go func() {
		buf := make([]byte, 1)
		_, readErr = conn.Read(buf)
		close(done)
	}()

	select {
	case <-done:
	case <-ctx.Done():
		_ = conn.SetReadDeadline(time.Now())
		<-done
	}

	result.Latency = time.Since(start)

	if ctx.Err() != nil {
		result.Status = HealthDown
		result.Error = ctx.Err().Error()
		return result
	}

	if readErr == nil {
		// Backend sent a response.
		result.Status = HealthHealthy
		return result
	}

	// Timeout means no ICMP error arrived — likely reachable.
	var netErr net.Error
	if errors.As(readErr, &netErr) {
		result.Status = HealthHealthy
		return result
	}

	// Any other error (e.g. ICMP port unreachable) means down.
	result.Status = HealthDown
	result.Error = readErr.Error()
	return result
}

// GetHealth returns the latest health check result.
func (hc *healthChecker) GetHealth() HealthResult {
	if hc == nil {
		return HealthResult{Status: HealthUnknown}
	}
	r := hc.result.Load()
	if r == nil {
		return HealthResult{Status: HealthUnknown}
	}
	return *r
}

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

func (proxy *Proxy) startHealthChecker() {
	if !proxy.Config.HealthCheckEnabled {
		if proxy.metrics != nil {
			proxy.metrics.SetProxyUp(proxy.Config.Hostname, -1)
		}
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

		if proxy.metrics != nil {
			hostname := proxy.Config.Hostname
			hc.onResult = func(result HealthResult) {
				switch result.Status {
				case HealthHealthy:
					proxy.metrics.SetProxyUp(hostname, 1)
				case HealthDown:
					proxy.metrics.SetProxyUp(hostname, 0)
				default:
					proxy.metrics.SetProxyUp(hostname, -1)
				}
			}
		}

		proxy.mtx.Lock()
		proxy.healthPortName = k
		proxy.health = hc
		proxy.mtx.Unlock()

		if proxy.metrics != nil {
			proxy.metrics.SetProxyUp(proxy.Config.Hostname, -1)
		}

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
