// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package metrics

import (
	"bufio"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var errHijackNotSupported = errors.New("hijack not supported")

// Metrics holds Prometheus metrics for per-proxy request instrumentation.
type Metrics struct {
	RequestsTotal    *prometheus.CounterVec
	RequestDuration  *prometheus.HistogramVec
	RequestsInFlight *prometheus.GaugeVec
	ProxiesTotal     prometheus.Gauge
	ProxyStatus      *prometheus.GaugeVec
	statusMu         sync.Mutex
}

const (
	labelProxy = "proxy"
	labelPort  = "port"
)

// New creates and registers Prometheus metrics with the default registerer.
func New() *Metrics {
	m := &Metrics{
		RequestsTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "tsdproxy_proxy_requests_total",
				Help: "Total number of requests proxied per proxy and port.",
			},
			[]string{labelProxy, labelPort},
		),
		RequestDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "tsdproxy_proxy_request_duration_seconds",
				Help:    "Histogram of request latencies per proxy and port.",
				Buckets: prometheus.DefBuckets,
			},
			[]string{labelProxy, labelPort, "code"},
		),
		RequestsInFlight: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "tsdproxy_proxy_requests_in_flight",
				Help: "Current number of in-flight requests per proxy and port.",
			},
			[]string{labelProxy, labelPort},
		),
		ProxiesTotal: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Name: "tsdproxy_proxies_total",
				Help: "Total number of active proxies.",
			},
		),
		ProxyStatus: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "tsdproxy_proxy_status",
				Help: "Current status of each proxy. Exactly one label combination per proxy has value 1.",
			},
			[]string{labelProxy, "status"},
		),
	}

	prometheus.DefaultRegisterer.MustRegister(
		m.RequestsTotal,
		m.RequestDuration,
		m.RequestsInFlight,
		m.ProxiesTotal,
		m.ProxyStatus,
	)

	return m
}

// Middleware returns an http.Handler middleware that records request metrics
// for the given proxy name and port name.
func (m *Metrics) Middleware(proxyName, portName string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			labels := prometheus.Labels{labelProxy: proxyName, labelPort: portName}

			m.RequestsTotal.With(labels).Inc()
			m.RequestsInFlight.With(labels).Inc()
			defer m.RequestsInFlight.With(labels).Dec()

			rec := &statusRecorder{ResponseWriter: w, statusCode: http.StatusOK}
			start := time.Now()

			next.ServeHTTP(rec, r)

			m.RequestDuration.With(prometheus.Labels{
				labelProxy: proxyName,
				labelPort:  portName,
				"code":     fmt.Sprintf("%d", rec.statusCode),
			}).Observe(time.Since(start).Seconds())
		})
	}
}

type statusRecorder struct {
	http.ResponseWriter
	statusCode int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.statusCode = code
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (r *statusRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	h, ok := r.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, errHijackNotSupported
	}
	return h.Hijack()
}

func (r *statusRecorder) Unwrap() http.ResponseWriter {
	return r.ResponseWriter
}

// Handler returns an http.Handler that serves the Prometheus metrics endpoint.
func (m *Metrics) Handler() http.Handler {
	return promhttp.Handler()
}

// SetProxyCount sets the total number of active proxies.
func (m *Metrics) SetProxyCount(count int) {
	m.ProxiesTotal.Set(float64(count))
}

// SetProxyStatus sets the status for a specific proxy.
func (m *Metrics) SetProxyStatus(proxyName, status string) {
	m.statusMu.Lock()
	m.ProxyStatus.DeletePartialMatch(prometheus.Labels{labelProxy: proxyName})
	m.ProxyStatus.With(prometheus.Labels{labelProxy: proxyName, "status": status}).Set(1)
	m.statusMu.Unlock()
}

// DeleteProxyMetrics removes all metrics for a proxy, including status entries.
func (m *Metrics) DeleteProxyMetrics(proxyName string) {
	m.RequestsTotal.DeletePartialMatch(prometheus.Labels{labelProxy: proxyName})
	m.RequestDuration.DeletePartialMatch(prometheus.Labels{labelProxy: proxyName})
	m.RequestsInFlight.DeletePartialMatch(prometheus.Labels{labelProxy: proxyName})
	m.statusMu.Lock()
	m.ProxyStatus.DeletePartialMatch(prometheus.Labels{labelProxy: proxyName})
	m.statusMu.Unlock()
}
