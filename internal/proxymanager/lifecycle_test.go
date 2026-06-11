// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package proxymanager

import (
	"context"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/rs/zerolog"

	"github.com/almeidapaulopt/tsdproxy/internal/config"
	"github.com/almeidapaulopt/tsdproxy/internal/model"
)

// -- NewProxyManager -----------------------------------------------------------

func TestNewProxyManager(t *testing.T) {
	pm := NewProxyManager(zerolog.Nop(), config.NewTestProvider().GetConfig(), "test-token")

	if pm.Proxies == nil {
		t.Fatal("expected Proxies map to be initialized")
	}
	if pm.TargetProviders == nil {
		t.Fatal("expected TargetProviders map to be initialized")
	}
	if pm.ProxyProviders == nil {
		t.Fatal("expected ProxyProviders map to be initialized")
	}
	if pm.DNSProviders == nil {
		t.Fatal("expected DNSProviders map to be initialized")
	}
	if pm.TLSProviders == nil {
		t.Fatal("expected TLSProviders map to be initialized")
	}
	if pm.statusSubscribers == nil {
		t.Fatal("expected statusSubscribers map to be initialized")
	}
	if pm.metrics == nil {
		t.Fatal("expected metrics to be initialized")
	}
}

// -- GetProxies / GetProxy -----------------------------------------------------

func TestGetProxies_Empty(t *testing.T) {
	t.Parallel()
	pm := newTestProxyManager(newTestConfig(t))
	proxies := pm.GetProxies()
	if len(proxies) != 0 {
		t.Fatalf("expected 0 proxies, got %d", len(proxies))
	}
}

func TestGetProxies_ReturnsClone(t *testing.T) {
	t.Parallel()
	pm := newTestProxyManager(newTestConfig(t))
	pm.Proxies["test"] = &Proxy{}

	proxies := pm.GetProxies()
	if len(proxies) != 1 {
		t.Fatalf("expected 1 proxy, got %d", len(proxies))
	}

	delete(proxies, "test")
	if len(pm.Proxies) != 1 {
		t.Fatal("GetProxies should return a clone")
	}
}

func TestGetProxy_Found(t *testing.T) {
	t.Parallel()
	pm := newTestProxyManager(newTestConfig(t))
	p := &Proxy{Config: &model.Config{Hostname: "test"}}
	pm.Proxies["test"] = p

	got, ok := pm.GetProxy("test")
	if !ok {
		t.Fatal("expected proxy to be found")
	}
	if got != p {
		t.Fatal("expected same proxy pointer")
	}
}

func TestGetProxy_NotFound(t *testing.T) {
	t.Parallel()
	pm := newTestProxyManager(newTestConfig(t))
	_, ok := pm.GetProxy("nonexistent")
	if ok {
		t.Fatal("expected proxy not found")
	}
}

// -- SubscribeStatusEvents / broadcastStatusEvents ----------------------------

func TestSubscribeStatusEvents_DeliversEvent(t *testing.T) {
	t.Parallel()
	pm := newTestProxyManager(newTestConfig(t))
	ch, cancel := pm.SubscribeStatusEvents()
	defer cancel()

	event := model.ProxyEvent{ID: "test", Status: model.ProxyStatusRunning, OldStatus: model.ProxyStatusStopped}
	pm.broadcastStatusEvents(event)

	select {
	case got := <-ch:
		if got.ID != "test" || got.Status != model.ProxyStatusRunning {
			t.Fatalf("unexpected event: %+v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event")
	}
}

func TestSubscribeStatusEvents_MultipleSubscribers(t *testing.T) {
	t.Parallel()
	pm := newTestProxyManager(newTestConfig(t))
	ch1, cancel1 := pm.SubscribeStatusEvents()
	defer cancel1()
	ch2, cancel2 := pm.SubscribeStatusEvents()
	defer cancel2()

	event := model.ProxyEvent{ID: "multi", Status: model.ProxyStatusRunning}
	pm.broadcastStatusEvents(event)

	for i, ch := range []<-chan model.ProxyEvent{ch1, ch2} {
		select {
		case got := <-ch:
			if got.ID != "multi" {
				t.Fatalf("subscriber %d: unexpected event ID: %s", i, got.ID)
			}
		case <-time.After(time.Second):
			t.Fatalf("subscriber %d: timed out", i)
		}
	}
}

func TestSubscribeStatusEvents_CancelStopsDelivery(t *testing.T) {
	t.Parallel()
	pm := newTestProxyManager(newTestConfig(t))
	ch, cancel := pm.SubscribeStatusEvents()
	cancel()

	event := model.ProxyEvent{ID: "cancel-test", Status: model.ProxyStatusRunning}
	pm.broadcastStatusEvents(event)

	select {
	case _, ok := <-ch:
		if ok {
			t.Fatal("expected channel to be closed after cancel")
		}
	default:
	}
}

func TestSubscribeStatusEvents_DropOnFullBuffer(t *testing.T) {
	t.Parallel()
	pm := newTestProxyManager(newTestConfig(t))
	ch, cancel := pm.SubscribeStatusEvents()
	defer cancel()

	for i := 0; i < 65; i++ {
		pm.broadcastStatusEvents(model.ProxyEvent{ID: "drop-test", Status: model.ProxyStatusRunning})
	}

	count := 0
	for {
		select {
		case <-ch:
			count++
		default:
			if count > 64 {
				t.Fatalf("expected at most 64 events, got %d", count)
			}
			return
		}
	}
}

// -- PauseProxy / ResumeProxy --------------------------------------------------

func TestPauseProxy_NotFound(t *testing.T) {
	t.Parallel()
	pm := newTestProxyManager(newTestConfig(t))
	err := pm.PauseProxy("nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent proxy")
	}
}

func TestResumeProxy_NotFound(t *testing.T) {
	t.Parallel()
	pm := newTestProxyManager(newTestConfig(t))
	err := pm.ResumeProxy("nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent proxy")
	}
}

func TestRestartProxy_NotFound(t *testing.T) {
	t.Parallel()
	pm := newTestProxyManager(newTestConfig(t))
	err := pm.RestartProxy("nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent proxy")
	}
}

// -- getTargetLock -------------------------------------------------------------

func TestGetTargetLock_SameIDReturnsSameMutex(t *testing.T) {
	t.Parallel()
	pm := newTestProxyManager(newTestConfig(t))
	mu1 := pm.getTargetLock("target1")
	mu2 := pm.getTargetLock("target1")
	if mu1 != mu2 {
		t.Fatal("expected same mutex for same target ID")
	}
}

func TestGetTargetLock_DifferentIDsReturnDifferentMutexes(t *testing.T) {
	t.Parallel()
	pm := newTestProxyManager(newTestConfig(t))
	mu1 := pm.getTargetLock("target1")
	mu2 := pm.getTargetLock("target2")
	if mu1 == mu2 {
		t.Fatal("expected different mutexes for different target IDs")
	}
}

// -- removeProxy --------------------------------------------------------------

func TestRemoveProxy_NotFound(t *testing.T) {
	pm := newTestProxyManager(newTestConfig(t))
	pm.removeProxy("nonexistent")
}

func TestRemoveProxy_RemovesExistingProxy(t *testing.T) {
	t.Parallel()
	pm := newTestProxyManager(newTestConfig(t))
	ctx, cancel := context.WithCancel(context.Background())
	pm.Proxies["test"] = &Proxy{
		log:           zerolog.Nop(),
		Config:        &model.Config{Hostname: "test"},
		metrics:       pm.metrics,
		providerProxy: &stubProviderProxy{},
		ctx:           ctx,
		cancel:        cancel,
		ports:         make(map[string]portHandler),
		statusHistory: make([]StatusTransition, 0, maxStatusHistory),
	}

	pm.removeProxy("test")
	if _, ok := pm.Proxies["test"]; ok {
		t.Fatal("expected proxy to be removed")
	}
}

func TestNewProxyManager_Start(t *testing.T) {
	oldReg := prometheus.DefaultRegisterer
	oldGatherer := prometheus.DefaultGatherer
	reg := prometheus.NewRegistry()
	prometheus.DefaultRegisterer = reg
	prometheus.DefaultGatherer = reg
	t.Cleanup(func() {
		prometheus.DefaultRegisterer = oldReg
		prometheus.DefaultGatherer = oldGatherer
	})

	pm := NewProxyManager(zerolog.Nop(), config.NewTestProvider().GetConfig(), "test-token")
	pm.Start()
}

func TestMetricsHandler(t *testing.T) {
	oldReg := prometheus.DefaultRegisterer
	oldGatherer := prometheus.DefaultGatherer
	reg := prometheus.NewRegistry()
	prometheus.DefaultRegisterer = reg
	prometheus.DefaultGatherer = reg
	t.Cleanup(func() {
		prometheus.DefaultRegisterer = oldReg
		prometheus.DefaultGatherer = oldGatherer
	})

	pm := NewProxyManager(zerolog.Nop(), config.NewTestProvider().GetConfig(), "test-token")

	h := pm.MetricsHandler()
	if h == nil {
		t.Fatal("expected non-nil metrics handler")
	}
}
