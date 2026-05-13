// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package proxymanager

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog"
)

// HealthStatus represents the health of a proxy's backend target.
type HealthStatus int

const (
	HealthUnknown HealthStatus = iota
	HealthHealthy
	HealthDown
)

func (s HealthStatus) String() string {
	switch s {
	case HealthHealthy:
		return "healthy"
	case HealthDown:
		return "down"
	default:
		return "unknown"
	}
}

// HealthResult holds the latest health check result for a proxy target.
type HealthResult struct {
	Status   HealthStatus
	Latency  time.Duration
	Error    string
	CheckedAt time.Time
}

// healthChecker probes a proxy's backend target on a fixed interval.
type healthChecker struct {
	log       zerolog.Logger
	target    string // host:port for TCP, full URL for HTTP
	scheme    string // "http", "https", "tcp"
	result    atomic.Pointer[HealthResult]
	ctx       context.Context
	cancel    context.CancelFunc
}

const (
	healthCheckInterval = 30 * time.Second
	healthCheckTimeout  = 5 * time.Second
)

func newHealthChecker(log zerolog.Logger, target, scheme string) *healthChecker {
	ctx, cancel := context.WithCancel(context.Background())

	hc := &healthChecker{
		log:    log.With().Str("component", "health").Logger(),
		target: target,
		scheme: scheme,
		ctx:    ctx,
		cancel: cancel,
	}

	hc.result.Store(&HealthResult{Status: HealthUnknown})

	return hc
}

func (hc *healthChecker) start() {
	go hc.run()
}

func (hc *healthChecker) stop() {
	hc.cancel()
}

func (hc *healthChecker) run() {
	hc.check()

	ticker := time.NewTicker(healthCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-hc.ctx.Done():
			return
		case <-ticker.C:
			hc.check()
		}
	}
}

func (hc *healthChecker) check() {
	ctx, cancel := context.WithTimeout(hc.ctx, healthCheckTimeout)
	defer cancel()

	var result HealthResult
	result.CheckedAt = time.Now()

	switch hc.scheme {
	case "http", "https":
		result = hc.checkHTTP(ctx)
	case "udp":
		result = hc.checkUDP(ctx)
	default:
		result = hc.checkTCP(ctx)
	}

	hc.result.Store(&result)

	hc.log.Debug().
		Str("status", result.Status.String()).
		Dur("latency", result.Latency).
		Msg("health check completed")
}

func (hc *healthChecker) checkHTTP(ctx context.Context) HealthResult {
	var result HealthResult
	result.CheckedAt = time.Now()

	client := &http.Client{
		Timeout: healthCheckTimeout,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint
			DialContext: (&net.Dialer{Timeout: healthCheckTimeout}).DialContext,
		},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			// Don't follow redirects — a 3xx still means the backend is up
			return http.ErrUseLastResponse
		},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, hc.target, nil)
	if err != nil {
		result.Status = HealthDown
		result.Error = fmt.Sprintf("invalid target URL: %v", err)
		return result
	}

	start := time.Now()
	resp, err := client.Do(req)
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
	conn, err := d.DialContext(ctx, "tcp", hc.target)
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

	addr, err := net.ResolveUDPAddr("udp", hc.target)
	if err != nil {
		result.Status = HealthDown
		result.Error = fmt.Sprintf("error resolving address: %v", err)
		return result
	}

	conn, err := net.DialUDP("udp", nil, addr)
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

	// Wait for either a response or an ICMP error.
	if err := conn.SetReadDeadline(time.Now().Add(healthCheckTimeout)); err != nil {
		result.Latency = time.Since(start)
		result.Status = HealthDown
		result.Error = err.Error()
		return result
	}

	buf := make([]byte, 1)
	_, readErr := conn.Read(buf)
	result.Latency = time.Since(start)

	if readErr == nil {
		// Backend sent a response.
		result.Status = HealthHealthy
		return result
	}

	// Timeout means no ICMP error arrived — likely reachable.
	if netErr, ok := readErr.(net.Error); ok && netErr.Timeout() {
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
