// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package metrics

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	dto "github.com/prometheus/client_model/go"

	"github.com/prometheus/client_golang/prometheus"
)

func newTestMetrics(t *testing.T) *Metrics {
	t.Helper()
	reg := prometheus.NewRegistry()
	return New(reg)
}

func TestNew(t *testing.T) {
	m := newTestMetrics(t)
	if m == nil {
		t.Fatal("New() returned nil")
	}
	if m.RequestsTotal == nil {
		t.Error("RequestsTotal not initialized")
	}
	if m.RequestDuration == nil {
		t.Error("RequestDuration not initialized")
	}
	if m.RequestsInFlight == nil {
		t.Error("RequestsInFlight not initialized")
	}
	if m.ProxiesTotal == nil {
		t.Error("ProxiesTotal not initialized")
	}
	if m.ProxyStatus == nil {
		t.Error("ProxyStatus not initialized")
	}
	if m.ProxyUp == nil {
		t.Error("ProxyUp not initialized")
	}
	if m.ConnectionsActive == nil {
		t.Error("ConnectionsActive not initialized")
	}
	if m.UDPClientsActive == nil {
		t.Error("UDPClientsActive not initialized")
	}
	if m.CertExpirySeconds == nil {
		t.Error("CertExpirySeconds not initialized")
	}
}

func TestSetProxyCount(t *testing.T) {
	m := newTestMetrics(t)
	m.SetProxyCount(5)
	m.SetProxyCount(0)
	m.SetProxyCount(100)
}

func TestSetProxyStatus(t *testing.T) {
	m := newTestMetrics(t)
	m.SetProxyStatus("webapp", "active")
	m.SetProxyStatus("webapp", "error")
	m.SetProxyStatus("webapp", "paused")
}

func TestSetProxyStatus_OverwriteRemovesOldStatus(t *testing.T) {
	m := newTestMetrics(t)
	m.SetProxyStatus("svc1", "active")
	m.SetProxyStatus("svc1", "error")

	got := collectGaugeValues(m.ProxyStatus)
	if got[`proxy="svc1",status="active"`] != 0 {
		t.Error("old status label was not cleaned up")
	}
	if got[`proxy="svc1",status="error"`] != 1 {
		t.Error("new status label was not set to 1")
	}
}

func TestDeleteProxyMetrics(t *testing.T) {
	m := newTestMetrics(t)
	m.SetProxyCount(3)
	m.SetProxyStatus("webapp", "active")
	m.SetProxyStatus("api", "error")

	m.DeleteProxyMetrics("webapp")

	got := collectGaugeValues(m.ProxyStatus)
	if got[`proxy="api",status="error"`] != 1 {
		t.Error("api proxy status should still exist")
	}
}

func TestDeleteProxyMetrics_NonExistent(t *testing.T) {
	m := newTestMetrics(t)
	m.DeleteProxyMetrics("nonexistent")
}

func TestSetProxyUp(t *testing.T) {
	m := newTestMetrics(t)
	m.SetProxyUp("webapp", 1)
	m.SetProxyUp("webapp", 0)
	m.SetProxyUp("webapp", -1)
}

func TestSetConnectionsActive(t *testing.T) {
	m := newTestMetrics(t)
	m.SetConnectionsActive("webapp", "443", 5)
	m.SetConnectionsActive("webapp", "443", 0)
}

func TestSetUDPClientsActive(t *testing.T) {
	m := newTestMetrics(t)
	m.SetUDPClientsActive("webapp", "5060", 3)
	m.SetUDPClientsActive("webapp", "5060", 0)
}

func TestSetCertExpirySeconds(t *testing.T) {
	m := newTestMetrics(t)
	m.SetCertExpirySeconds("webapp", 86400)
	m.SetCertExpirySeconds("webapp", 0)
}

func TestMiddleware(t *testing.T) {
	m := newTestMetrics(t)
	mw := m.Middleware("testproxy", "443")

	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if body := rec.Body.String(); body != "ok" {
		t.Fatalf("expected body 'ok', got %q", body)
	}
}

func TestMiddleware_RecordsMetrics(t *testing.T) {
	m := newTestMetrics(t)
	mw := m.Middleware("testproxy", "443")

	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("not found"))
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	val := m.RequestsTotal.WithLabelValues("testproxy", "443", "404")
	c := collectCounterValue(val) > 0
	if !c {
		t.Error("expected requests total to be > 0")
	}
}

func TestMiddleware_Flusher(t *testing.T) {
	m := newTestMetrics(t)
	mw := m.Middleware("testproxy", "443")

	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("expected http.Flusher interface")
		}
		_, _ = w.Write([]byte("partial"))
		flusher.Flush()
		_, _ = w.Write([]byte("done"))
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
}

func TestMiddleware_ResponseWriterWrapper(t *testing.T) {
	m := newTestMetrics(t)
	mw := m.Middleware("testproxy", "443")

	var inner http.ResponseWriter
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		inner = w
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	sr, ok := inner.(*statusRecorder)
	if !ok {
		t.Fatal("handler did not receive a statusRecorder")
	}
	if sr.statusCode != http.StatusOK {
		t.Errorf("expected default status 200, got %d", sr.statusCode)
	}
}

func TestStatusRecorder_WriteHeader(t *testing.T) {
	sr := &statusRecorder{ResponseWriter: httptest.NewRecorder()}
	sr.WriteHeader(http.StatusTeapot)

	if sr.statusCode != http.StatusTeapot {
		t.Errorf("statusCode = %d, want %d", sr.statusCode, http.StatusTeapot)
	}
}

func TestStatusRecorder_WriteHeaderOverwrites(t *testing.T) {
	sr := &statusRecorder{ResponseWriter: httptest.NewRecorder()}
	sr.WriteHeader(http.StatusTeapot)
	sr.WriteHeader(http.StatusOK)

	// statusRecorder does not guard against multiple WriteHeader calls;
	// the last one wins (wrapping the underlying ResponseWriter behavior).
	if sr.statusCode != http.StatusOK {
		t.Errorf("expected last WriteHeader value, got %d", sr.statusCode)
	}
}

func TestStatusRecorder_Flush(_ *testing.T) {
	sr := &statusRecorder{ResponseWriter: httptest.NewRecorder()}
	sr.Flush()
}

func TestStatusRecorder_Unwrap(t *testing.T) {
	inner := httptest.NewRecorder()
	sr := &statusRecorder{ResponseWriter: inner}

	if unwrapped := sr.Unwrap(); unwrapped != inner {
		t.Error("Unwrap() did not return the original ResponseWriter")
	}
}

func TestHandler(t *testing.T) {
	m := newTestMetrics(t)
	handler := m.Handler()

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "# HELP") {
		t.Error("expected Prometheus HELP lines in response")
	}
	if !strings.Contains(body, "tsdproxy_proxies") {
		t.Error("expected tsdproxy_proxies metric in response")
	}
}

func TestHandler_IncludesProxyStatus(t *testing.T) {
	m := newTestMetrics(t)
	m.SetProxyStatus("webapp", "active")
	m.SetProxyStatus("api", "error")

	handler := m.Handler()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, `tsdproxy_proxy_status{proxy="webapp",status="active"}`) {
		t.Error("expected webapp active status in metrics output")
	}
	if !strings.Contains(body, `tsdproxy_proxy_status{proxy="api",status="error"}`) {
		t.Error("expected api error status in metrics output")
	}
}

func TestHandler_IncludesNewMetrics(t *testing.T) {
	m := newTestMetrics(t)
	m.SetProxyUp("webapp", 1)
	m.SetConnectionsActive("webapp", "443", 3)
	m.SetUDPClientsActive("webapp", "5060", 2)
	m.SetCertExpirySeconds("webapp", 86400)

	handler := m.Handler()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	body := rec.Body.String()

	if !strings.Contains(body, "tsdproxy_proxy_up") {
		t.Error("expected proxy_up metric in output")
	}
	if !strings.Contains(body, "tsdproxy_proxy_connections_active") {
		t.Error("expected connections_active metric in output")
	}
	if !strings.Contains(body, "tsdproxy_udp_clients_active") {
		t.Error("expected udp_clients_active metric in output")
	}
	if !strings.Contains(body, `tsdproxy_cert_expiry_seconds{proxy="webapp"}`) {
		t.Error("expected cert_expiry_seconds metric in output", body)
	}
}

func TestConcurrentSetProxyStatus(t *testing.T) {
	m := newTestMetrics(t)
	var wg sync.WaitGroup

	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			name := "proxy-" + string(rune('A'+n%5))
			statuses := []string{"active", "error", "paused"}
			status := statuses[n%len(statuses)]
			m.SetProxyStatus(name, status)
		}(i)
	}
	wg.Wait()
}

func TestConcurrentDeleteProxyMetrics(t *testing.T) {
	m := newTestMetrics(t)
	var wg sync.WaitGroup

	for i := 0; i < 10; i++ {
		m.SetProxyStatus("proxy-"+string(rune('A'+i)), "active")
	}

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			name := "proxy-" + string(rune('A'+n)) //nolint:gosec
			m.DeleteProxyMetrics(name)
		}(i)
	}
	wg.Wait()
}

// collectGaugeValues returns a map of label string to value for the given GaugeVec.
func collectGaugeValues(g *prometheus.GaugeVec) map[string]float64 {
	ch := make(chan prometheus.Metric, 100)
	g.Collect(ch)
	close(ch)

	out := make(map[string]float64)
	for metric := range ch {
		var pb dto.Metric
		if err := metric.Write(&pb); err != nil {
			continue
		}
		val := 0.0
		if pb.Gauge != nil {
			val = pb.Gauge.GetValue()
		}
		labels := make(map[string]string)
		for _, l := range pb.Label {
			labels[l.GetName()] = l.GetValue()
		}
		key := fmt.Sprintf(`proxy=%q,status=%q`, labels["proxy"], labels["status"])
		out[key] = val
	}
	return out
}

// collectCounterValue returns the current value of a Counter.
func collectCounterValue(c prometheus.Counter) float64 {
	var pb dto.Metric
	if err := c.Write(&pb); err != nil {
		return 0
	}
	if pb.Counter != nil {
		return pb.Counter.GetValue()
	}
	return 0
}
