// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package proxymanager

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/rs/zerolog"

	"github.com/almeidapaulopt/tsdproxy/internal/core/metrics"
	"github.com/almeidapaulopt/tsdproxy/internal/model"
)

// These tests verify two metric-lifecycle bugs flagged in the uncommitted
// metrics-integration review:
//
//   M1 — Pause() stops health checks and closes port listeners but never
//        resets ProxyUp / ConnectionsActive / UDPClientsActive. The metrics
//        retain stale values from before pause, misreporting a paused proxy
//        as "healthy with active connections".
//
//   M2 — udpPort.close() drains clientMap via closeAllClients but never
//        calls updateClientMetric(0). The UDPClientsActive gauge keeps its
//        last non-zero value indefinitely.
//
// Both tests are designed to FAIL while the bug exists and PASS once fixed.

// readGaugeByLabels returns the current value of a GaugeVec series matching
// the given label set. Returns (value, found).
func readGaugeByLabels(g *prometheus.GaugeVec, want map[string]string) (float64, bool) {
	ch := make(chan prometheus.Metric, 8)
	g.Collect(ch)
	close(ch)
	for metric := range ch {
		var pb dto.Metric
		if err := metric.Write(&pb); err != nil {
			continue
		}
		if len(pb.Label) != len(want) {
			continue
		}
		match := true
		for _, l := range pb.Label {
			if want[l.GetName()] != l.GetValue() {
				match = false
				break
			}
		}
		if !match {
			continue
		}
		if pb.Gauge != nil {
			return pb.Gauge.GetValue(), true
		}
	}
	return 0, false
}

// mockPortHandler is a no-op portHandler used to satisfy Proxy.ports so
// Pause()/closePorts() has something to iterate without panicking.
type mockPortHandler struct {
	closed bool
}

func (m *mockPortHandler) startWithListener(_ net.Listener) error { return nil }
func (m *mockPortHandler) close() error {
	m.closed = true
	return nil
}

// -----------------------------------------------------------------------------
// M1: Pause() does not reset health / connection metrics
// -----------------------------------------------------------------------------

// TestM1_PauseDoesNotResetHealthAndConnectionMetrics proves that Pause()
// leaves ProxyUp, ConnectionsActive, and UDPClientsActive at their pre-pause
// values even though the proxy is no longer serving traffic.
//
// Real-world impact: a paused proxy still reports "healthy" with N active
// connections in Prometheus/Grafana, misleading operators.
//
// Assertion strategy: this test FAILS while the bug exists and PASSES once
// Pause() is fixed to zero out the per-port/health gauges.
func TestM1_PauseDoesNotResetHealthAndConnectionMetrics(t *testing.T) {
	m := metrics.New(prometheus.NewRegistry())

	handler := &mockPortHandler{}
	proxy := &Proxy{
		log:          zerolog.Nop(),
		Config:       &model.Config{Hostname: "testproxy"},
		ctx:          context.Background(),
		metrics:      m,
		ports:        map[string]portHandler{"443": handler},
		status:       model.ProxyStatusRunning,
		metricsReady: true,
	}

	// Simulate a healthy proxy with active traffic.
	m.SetProxyUp("testproxy", 1)
	m.SetConnectionsActive("testproxy", "443", 5)
	m.SetUDPClientsActive("testproxy", "5060", 3)

	// Sanity check: values are set as expected.
	if got, _ := readGaugeByLabels(m.ProxyUp, map[string]string{"proxy": "testproxy"}); got != 1 {
		t.Fatalf("sanity check: ProxyUp want 1, got %v", got)
	}
	if got, _ := readGaugeByLabels(m.ConnectionsActive, map[string]string{"proxy": "testproxy", "port": "443"}); got != 5 {
		t.Fatalf("sanity check: ConnectionsActive want 5, got %v", got)
	}
	if got, _ := readGaugeByLabels(m.UDPClientsActive, map[string]string{"proxy": "testproxy", "port": "5060"}); got != 3 {
		t.Fatalf("sanity check: UDPClientsActive want 3, got %v", got)
	}

	// Act: pause the proxy. This stops health checks and closes all ports.
	if err := proxy.Pause(); err != nil {
		t.Fatalf("Pause failed: %v", err)
	}

	if !handler.closed {
		t.Fatal("setup invariant failed: mock port handler was not closed by Pause()")
	}

	// Assert: after Pause, the proxy is not serving — metrics should reflect that.
	//
	// Expected post-fix behavior:
	//   - ProxyUp            → -1   (health checker stopped, state unknown)
	//   - ConnectionsActive  →  0   (listeners closed, no in-flight conns)
	//   - UDPClientsActive   →  0   (packet conn closed, no clients)
	//
	// While the bug exists, these retain their pre-pause values.

	t.Run("ProxyUp_should_be_unknown", func(t *testing.T) {
		got, ok := readGaugeByLabels(m.ProxyUp, map[string]string{"proxy": "testproxy"})
		if !ok {
			t.Fatal("ProxyUp metric disappeared after Pause")
		}
		if got != -1 {
			t.Errorf("M1 CONFIRMED (ProxyUp): after Pause, expected -1 (unknown), got %v. "+
				"Health checker is stopped — metric should reflect unknown state, "+
				"but it still reports the pre-pause value.", got)
		}
	})

	t.Run("ConnectionsActive_should_be_zero", func(t *testing.T) {
		got, ok := readGaugeByLabels(m.ConnectionsActive, map[string]string{"proxy": "testproxy", "port": "443"})
		if !ok {
			t.Fatal("ConnectionsActive metric disappeared after Pause")
		}
		if got != 0 {
			t.Errorf("M1 CONFIRMED (ConnectionsActive): after Pause, expected 0, got %v. "+
				"closePorts() shut down all listeners — metric should be 0, "+
				"but it still reports the pre-pause value.", got)
		}
	})

	t.Run("UDPClientsActive_should_be_zero", func(t *testing.T) {
		got, ok := readGaugeByLabels(m.UDPClientsActive, map[string]string{"proxy": "testproxy", "port": "5060"})
		if !ok {
			t.Fatal("UDPClientsActive metric disappeared after Pause")
		}
		if got != 0 {
			t.Errorf("M1 CONFIRMED (UDPClientsActive): after Pause, expected 0, got %v. "+
				"closePorts() shut down all packet conns — metric should be 0, "+
				"but it still reports the pre-pause value.", got)
		}
	})
}

// -----------------------------------------------------------------------------
// M2: udpPort.close() does not reset UDPClientsActive
// -----------------------------------------------------------------------------

// TestM2_UDPPortCloseDoesNotResetClientMetric proves that closing a UDP port
// leaves UDPClientsActive at its last value because closeAllClients drains
// clientMap without calling updateClientMetric(0).
//
// Real-world impact: during Pause→Resume cycles (where the proxy stays
// registered in Prometheus), the UDP client count is a sticky non-zero
// number that never decreases.
//
// Assertion strategy: this test FAILS while the bug exists and PASSES once
// udpPort.close() is fixed to zero the gauge after draining clients.
func TestM2_UDPPortCloseDoesNotResetClientMetric(t *testing.T) {
	m := metrics.New(prometheus.NewRegistry())
	up := newPortUDP(
		context.Background(),
		model.PortConfig{ProxyProtocol: model.ProtoUDP},
		zerolog.Nop(),
		m,
		"testproxy",
		"5060",
	)
	t.Cleanup(func() { _ = up.close() })

	// Populate clientMap as if 3 UDP clients were active. Production code
	// creates these via getOrCreateBackendConn on packet receive.
	up.clientMapMtx.Lock()
	up.clientMap = map[string]*clientEntry{
		"127.0.0.1:5001": {lastSeen: time.Now()},
		"127.0.0.1:5002": {lastSeen: time.Now()},
		"127.0.0.1:5003": {lastSeen: time.Now()},
	}
	up.clientMapMtx.Unlock()

	// Sync the metric with the map state, exactly as production code does
	// after each getOrCreateBackendConn / relayBackendToClient lifecycle.
	up.updateClientMetric(3)

	// Sanity check: metric reflects the 3 active clients.
	got, ok := readGaugeByLabels(m.UDPClientsActive, map[string]string{"proxy": "testproxy", "port": "5060"})
	if !ok || got != 3 {
		t.Fatalf("sanity check: UDPClientsActive want 3, got %v (found=%v)", got, ok)
	}

	// Act: close the UDP port. This drains clientMap via closeAllClients.
	if err := up.close(); err != nil {
		t.Fatalf("udpPort.close() failed: %v", err)
	}

	// Assert: UDPClientsActive should be 0 because the port is closed and
	// the map is empty.
	//
	// While the bug exists, the metric retains its last value (3) because
	// closeAllClients does not call updateClientMetric(0).
	got, ok = readGaugeByLabels(m.UDPClientsActive, map[string]string{"proxy": "testproxy", "port": "5060"})
	if !ok {
		t.Fatal("M2: UDPClientsActive metric series disappeared after close. " +
			"This happens if the registry deletes unreferenced series, which " +
			"Prometheus does NOT do — the series should persist with value 0.")
	}
	if got != 0 {
		t.Errorf("M2 CONFIRMED: UDPClientsActive retained stale value %v after udpPort.close() "+
			"(closeAllClients closes each entry's conn but never calls updateClientMetric(0)). "+
			"On Pause→Resume cycles this leaks stale client counts indefinitely.", got)
	}
}
