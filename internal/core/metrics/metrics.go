// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package metrics

import (
	"bufio"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	dto "github.com/prometheus/client_model/go"

	"github.com/almeidapaulopt/tsdproxy/internal/core"
)

// Metrics holds Prometheus metrics for per-proxy request instrumentation.
type Metrics struct {
	ProxiesTotal      prometheus.Gauge
	reg               prometheus.Registerer
	gatherer          prometheus.Gatherer
	RequestsTotal     *prometheus.CounterVec
	RequestDuration   *prometheus.HistogramVec
	RequestsInFlight  *prometheus.GaugeVec
	ProxyStatus       *prometheus.GaugeVec
	ProxyUp           *prometheus.GaugeVec
	ConnectionsActive *prometheus.GaugeVec
	UDPClientsActive  *prometheus.GaugeVec
	CertExpirySeconds *prometheus.GaugeVec
	statusMu          sync.Mutex
}

const (
	labelProxy = "proxy"
	labelPort  = "port"
	labelCode  = "code"
)

// New creates and registers Prometheus metrics with the given registerer.
// If reg is nil, prometheus.DefaultRegisterer is used.
func New(reg prometheus.Registerer) *Metrics {
	if reg == nil {
		reg = prometheus.DefaultRegisterer
	}

	var gatherer prometheus.Gatherer
	if g, ok := reg.(prometheus.Gatherer); ok {
		gatherer = g
	} else {
		gatherer = prometheus.DefaultGatherer
	}

	m := &Metrics{
		reg:      reg,
		gatherer: gatherer,
		RequestsTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "tsdproxy_proxy_requests_total",
				Help: "Total number of requests proxied per proxy, port, and status code.",
			},
			[]string{labelProxy, labelPort, labelCode},
		),
		RequestDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "tsdproxy_proxy_request_duration_seconds",
				Help:    "Histogram of request latencies per proxy and port.",
				Buckets: prometheus.DefBuckets,
			},
			[]string{labelProxy, labelPort, labelCode},
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
				Name: "tsdproxy_proxies",
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
		ProxyUp: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "tsdproxy_proxy_up",
				Help: "Health status of each proxy: 1 if healthy, 0 if down, -1 if unknown.",
			},
			[]string{labelProxy},
		),
		ConnectionsActive: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "tsdproxy_proxy_connections_active",
				Help: "Current number of active TCP connections per proxy and port.",
			},
			[]string{labelProxy, labelPort},
		),
		UDPClientsActive: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "tsdproxy_udp_clients_active",
				Help: "Current number of concurrent UDP clients per proxy and port.",
			},
			[]string{labelProxy, labelPort},
		),
		CertExpirySeconds: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "tsdproxy_cert_expiry_seconds",
				Help: "TLS certificate remaining lifetime in seconds per proxy.",
			},
			[]string{labelProxy},
		),
	}

	reg.MustRegister(
		m.RequestsTotal,
		m.RequestDuration,
		m.RequestsInFlight,
		m.ProxiesTotal,
		m.ProxyStatus,
		m.ProxyUp,
		m.ConnectionsActive,
		m.UDPClientsActive,
		m.CertExpirySeconds,
	)

	return m
}

// Middleware returns an http.Handler middleware that records request metrics
// for the given proxy name and port name.
func (m *Metrics) Middleware(proxyName, portName string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			baseLabels := prometheus.Labels{labelProxy: proxyName, labelPort: portName}

			m.RequestsInFlight.With(baseLabels).Inc()
			defer m.RequestsInFlight.With(baseLabels).Dec()

			rec := &statusRecorder{ResponseWriter: w, statusCode: http.StatusOK}
			start := time.Now()

			next.ServeHTTP(rec, r)

			code := strconv.Itoa(rec.statusCode)
			labels := prometheus.Labels{
				labelProxy: proxyName,
				labelPort:  portName,
				labelCode:  code,
			}

			m.RequestsTotal.With(labels).Inc()
			m.RequestDuration.With(labels).Observe(time.Since(start).Seconds())
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
		return nil, nil, core.ErrHijackNotSupported
	}
	return h.Hijack()
}

func (r *statusRecorder) Unwrap() http.ResponseWriter {
	return r.ResponseWriter
}

// Handler returns an http.Handler that serves the Prometheus metrics endpoint.
func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.gatherer, promhttp.HandlerOpts{})
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

// SetProxyUp sets the health status for a proxy: 1 if healthy, 0 if not.
func (m *Metrics) SetProxyUp(proxyName string, up int) {
	m.ProxyUp.With(prometheus.Labels{labelProxy: proxyName}).Set(float64(up))
}

// SetConnectionsActive sets the active connection count for a proxy port.
func (m *Metrics) SetConnectionsActive(proxyName, portName string, count int) {
	m.ConnectionsActive.With(prometheus.Labels{labelProxy: proxyName, labelPort: portName}).Set(float64(count))
}

// SetUDPClientsActive sets the active UDP client count for a proxy port.
func (m *Metrics) SetUDPClientsActive(proxyName, portName string, count int) {
	m.UDPClientsActive.With(prometheus.Labels{labelProxy: proxyName, labelPort: portName}).Set(float64(count))
}

// SetCertExpirySeconds sets the remaining TLS certificate lifetime in seconds for a proxy.
func (m *Metrics) SetCertExpirySeconds(proxyName string, seconds float64) {
	m.CertExpirySeconds.With(prometheus.Labels{labelProxy: proxyName}).Set(seconds)
}

// ResetProxyPortMetrics zeroes every ConnectionsActive and UDPClientsActive
// series registered for the given proxy, regardless of port name. Used by
// Proxy.Pause to avoid stale "active connection" counts while the proxy
// is not serving traffic.
func (m *Metrics) ResetProxyPortMetrics(proxyName string) {
	m.zeroPortGaugeByProxy(m.ConnectionsActive, proxyName)
	m.zeroPortGaugeByProxy(m.UDPClientsActive, proxyName)
}

// metricCollectBufferSize bounds the channel used when sweeping a GaugeVec
// for series to reset. It only needs to fit the series count for one proxy
// (typically <10 ports); 64 is a generous upper bound that prevents
// Collect from blocking on a slow consumer.
const metricCollectBufferSize = 64

// zeroPortGaugeByProxy inspects an existing {proxy, port} GaugeVec and
// sets every series matching proxyName to 0. It walks the currently
// registered series rather than relying on a separate port registry.
func (m *Metrics) zeroPortGaugeByProxy(g *prometheus.GaugeVec, proxyName string) {
	ch := make(chan prometheus.Metric, metricCollectBufferSize)
	g.Collect(ch)
	close(ch)
	for metric := range ch {
		var pb dto.Metric
		if err := metric.Write(&pb); err != nil {
			continue
		}
		port := ""
		proxyMatch := false
		for _, l := range pb.Label {
			switch l.GetName() {
			case labelProxy:
				proxyMatch = l.GetValue() == proxyName
			case labelPort:
				port = l.GetValue()
			}
		}
		if !proxyMatch || port == "" {
			continue
		}
		g.With(prometheus.Labels{labelProxy: proxyName, labelPort: port}).Set(0)
	}
}

// DeleteProxyMetrics removes all metrics for a proxy, including status entries.
func (m *Metrics) DeleteProxyMetrics(proxyName string) {
	m.RequestsTotal.DeletePartialMatch(prometheus.Labels{labelProxy: proxyName})
	m.RequestDuration.DeletePartialMatch(prometheus.Labels{labelProxy: proxyName})
	m.RequestsInFlight.DeletePartialMatch(prometheus.Labels{labelProxy: proxyName})
	m.ProxyUp.DeletePartialMatch(prometheus.Labels{labelProxy: proxyName})
	m.ConnectionsActive.DeletePartialMatch(prometheus.Labels{labelProxy: proxyName})
	m.UDPClientsActive.DeletePartialMatch(prometheus.Labels{labelProxy: proxyName})
	m.CertExpirySeconds.DeletePartialMatch(prometheus.Labels{labelProxy: proxyName})
	m.statusMu.Lock()
	m.ProxyStatus.DeletePartialMatch(prometheus.Labels{labelProxy: proxyName})
	m.statusMu.Unlock()
}
