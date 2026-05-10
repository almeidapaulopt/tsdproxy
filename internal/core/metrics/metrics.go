// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package metrics

import (
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Metrics holds Prometheus metrics for per-proxy request instrumentation.
type Metrics struct {
	RequestsTotal   *prometheus.CounterVec
	RequestDuration *prometheus.HistogramVec
	RequestsInFlight *prometheus.GaugeVec
}

// New creates and registers Prometheus metrics with the default registerer.
func New() *Metrics {
	m := &Metrics{
		RequestsTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "tsdproxy_proxy_requests_total",
				Help: "Total number of requests proxied per proxy and port.",
			},
			[]string{"proxy", "port"},
		),
		RequestDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "tsdproxy_proxy_request_duration_seconds",
				Help:    "Histogram of request latencies per proxy and port.",
				Buckets: prometheus.DefBuckets,
			},
			[]string{"proxy", "port", "code"},
		),
		RequestsInFlight: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "tsdproxy_proxy_requests_in_flight",
				Help: "Current number of in-flight requests per proxy and port.",
			},
			[]string{"proxy", "port"},
		),
	}

	prometheus.DefaultRegisterer.MustRegister(
		m.RequestsTotal,
		m.RequestDuration,
		m.RequestsInFlight,
	)

	return m
}

// Middleware returns an http.Handler middleware that records request metrics
// for the given proxy name and port name.
func (m *Metrics) Middleware(proxyName, portName string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			labels := prometheus.Labels{"proxy": proxyName, "port": portName}

			m.RequestsTotal.With(labels).Inc()
			m.RequestsInFlight.With(labels).Inc()
			defer m.RequestsInFlight.With(labels).Dec()

			start := time.Now()

			next.ServeHTTP(w, r)

			duration := time.Since(start).Seconds()
			m.RequestDuration.With(prometheus.Labels{
				"proxy": proxyName,
				"port":  portName,
				"code":  "200",
			}).Observe(duration)
		})
	}
}

// Handler returns an http.Handler that serves the Prometheus metrics endpoint.
func (m *Metrics) Handler() http.Handler {
	return promhttp.Handler()
}
