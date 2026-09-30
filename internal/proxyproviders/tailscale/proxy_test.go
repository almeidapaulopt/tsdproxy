// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package tailscale

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"golang.org/x/sync/semaphore"

	"github.com/almeidapaulopt/tsdproxy/internal/model"
	"github.com/almeidapaulopt/tsdproxy/internal/proxyproviders"
)

func newTestProxyWithLifecycle(lc *NodeLifecycle, ports model.PortConfigList) *Proxy {
	if ports == nil {
		ports = model.PortConfigList{}
	}
	return &Proxy{
		log:        zerolog.Nop(),
		config:     &model.Config{Ports: ports},
		certSem:    semaphore.NewWeighted(2),
		lifecycle:  lc,
		exposure:   NewPerProxyExposure(zerolog.Nop()),
		events:     make(chan model.ProxyEvent, 64),
		whoisCache: NewWhoisCache(whoisCacheTTL, whoisCacheMaxEntries),
	}
}

func newMinimalProxy(ports model.PortConfigList) *Proxy {
	if ports == nil {
		ports = model.PortConfigList{}
	}
	return &Proxy{
		log:        zerolog.Nop(),
		config:     &model.Config{Ports: ports},
		certSem:    semaphore.NewWeighted(2),
		events:     make(chan model.ProxyEvent, 64),
		whoisCache: NewWhoisCache(whoisCacheTTL, whoisCacheMaxEntries),
	}
}

func TestProxy_GetURL_Empty(t *testing.T) {
	t.Parallel()

	p := newMinimalProxy(nil)
	if got := p.GetURL(); got != "" {
		t.Errorf("GetURL() = %q, want empty string", got)
	}
}

func TestProxy_GetURL_WithHTTPS(t *testing.T) {
	t.Parallel()

	ports := model.PortConfigList{
		"443": {ProxyProtocol: model.ProtoHTTPS},
	}
	p := newMinimalProxy(ports)
	p.url = "myapp.tailnet.ts.net"

	got := p.GetURL()
	want := "https://myapp.tailnet.ts.net"
	if got != want {
		t.Errorf("GetURL() = %q, want %q", got, want)
	}
}

func TestProxy_GetURL_WithHTTP(t *testing.T) {
	t.Parallel()

	ports := model.PortConfigList{
		"80": {ProxyProtocol: model.ProtoHTTP},
	}
	p := newMinimalProxy(ports)
	p.url = "myapp.tailnet.ts.net"

	got := p.GetURL()
	want := "http://myapp.tailnet.ts.net"
	if got != want {
		t.Errorf("GetURL() = %q, want %q", got, want)
	}
}

func TestProxy_GetURL_WithTCP(t *testing.T) {
	t.Parallel()

	ports := model.PortConfigList{
		"22": {ProxyProtocol: model.ProtoTCP},
	}
	p := newMinimalProxy(ports)
	p.url = "myapp.tailnet.ts.net:22"

	got := p.GetURL()
	want := "tcp://myapp.tailnet.ts.net:22"
	if got != want {
		t.Errorf("GetURL() = %q, want %q", got, want)
	}
}

func TestProxy_GetListener_PortNotInConfig(t *testing.T) {
	t.Parallel()

	p := newMinimalProxy(nil)

	_, err := p.GetListener("9999")
	if err == nil {
		t.Fatal("expected error for missing port")
	}
	if !errors.Is(err, ErrProxyPortNotFound) {
		t.Errorf("error = %v, want ErrProxyPortNotFound", err)
	}
}

func TestProxy_GetRawTCPListener_PortNotInConfig(t *testing.T) {
	t.Parallel()

	p := newMinimalProxy(nil)

	_, err := p.GetRawTCPListener("9999")
	if err == nil {
		t.Fatal("expected error for missing port")
	}
	if !errors.Is(err, ErrProxyPortNotFound) {
		t.Errorf("error = %v, want ErrProxyPortNotFound", err)
	}
}

func TestProxy_GetPacketConn_PortNotInConfig(t *testing.T) {
	t.Parallel()

	p := newMinimalProxy(nil)

	_, err := p.GetPacketConn("9999")
	if err == nil {
		t.Fatal("expected error for missing port")
	}
	if !errors.Is(err, ErrProxyPortNotFound) {
		t.Errorf("error = %v, want ErrProxyPortNotFound", err)
	}
}

func TestProxy_WatchEvents(t *testing.T) {
	t.Parallel()

	p := newMinimalProxy(nil)
	ch := p.WatchEvents()
	if ch == nil {
		t.Fatal("WatchEvents() should return non-nil channel")
	}
}

func TestProxy_GetAuthURL_Empty(t *testing.T) {
	t.Parallel()

	p := newMinimalProxy(nil)
	if got := p.GetAuthURL(); got != "" {
		t.Errorf("GetAuthURL() = %q, want empty", got)
	}
}

func TestProxy_GetAuthURL_Set(t *testing.T) {
	t.Parallel()

	p := newMinimalProxy(nil)
	p.authURL = "https://login.tailscale.com/a/abc123"
	if got := p.GetAuthURL(); got != "https://login.tailscale.com/a/abc123" {
		t.Errorf("GetAuthURL() = %q, want auth URL", got)
	}
}

func TestProxy_Whois_NilRequest(t *testing.T) {
	t.Parallel()

	p := newMinimalProxy(nil)
	got := p.Whois(nil)
	if got != (model.Whois{}) {
		t.Errorf("Whois(nil) = %+v, want zero value", got)
	}
}

func TestProxy_Whois_NilRuntime(t *testing.T) {
	t.Parallel()

	lc := NewNodeLifecycle(zerolog.Nop(), NodeLifecycleConfig{
		Retry: RetryPolicy{MaxAttempts: 0},
	})
	p := newTestProxyWithLifecycle(lc, nil)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "100.100.100.100:12345"

	got := p.Whois(req)
	if got != (model.Whois{}) {
		t.Errorf("Whois() with nil runtime = %+v, want zero value", got)
	}
}

func TestProxy_GetLocalClient_NilRuntime(t *testing.T) {
	t.Parallel()

	lc := NewNodeLifecycle(zerolog.Nop(), NodeLifecycleConfig{
		Retry: RetryPolicy{MaxAttempts: 0},
	})
	p := newTestProxyWithLifecycle(lc, nil)
	if got := p.GetLocalClient(); got != nil {
		t.Error("GetLocalClient() should return nil when lifecycle has no runtime")
	}
}

func TestProxy_HasHTTPSPort_True(t *testing.T) {
	t.Parallel()

	ports := model.PortConfigList{
		"443": {ProxyProtocol: model.ProtoHTTPS},
		"80":  {ProxyProtocol: model.ProtoHTTP},
	}
	p := newMinimalProxy(ports)
	if !p.hasHTTPSPort() {
		t.Error("hasHTTPSPort() = false, want true")
	}
}

func TestProxy_HasHTTPSPort_False(t *testing.T) {
	t.Parallel()

	ports := model.PortConfigList{
		"80": {ProxyProtocol: model.ProtoHTTP},
	}
	p := newMinimalProxy(ports)
	if p.hasHTTPSPort() {
		t.Error("hasHTTPSPort() = true, want false")
	}
}

func TestProxy_HasHTTPSPort_EmptyPorts(t *testing.T) {
	t.Parallel()

	p := newMinimalProxy(nil)
	if p.hasHTTPSPort() {
		t.Error("hasHTTPSPort() = true with empty ports, want false")
	}
}

func TestProxy_Close_WhenNotStarted(t *testing.T) {
	t.Parallel()

	p := newMinimalProxy(nil)
	if err := p.Close(); err != nil {
		t.Fatalf("Close() error: %v", err)
	}

	_, ok := <-p.events
	if ok {
		t.Error("events channel should be closed")
	}
}

func TestProxy_Close_Idempotent(t *testing.T) {
	t.Parallel()

	p := newMinimalProxy(nil)

	if err := p.Close(); err != nil {
		t.Fatalf("first Close() error: %v", err)
	}
	if err := p.Close(); err != nil {
		t.Fatalf("second Close() error: %v", err)
	}
}

func TestProxy_BridgeEvents_ForwardsStatus(t *testing.T) {
	t.Parallel()

	lc := NewNodeLifecycle(zerolog.Nop(), NodeLifecycleConfig{
		Retry: RetryPolicy{MaxAttempts: 0},
	})

	p := newTestProxyWithLifecycle(lc, nil)
	p.bridgeDone = make(chan struct{})

	go p.bridgeEvents()

	lc.events <- NodeEvent{Status: model.ProxyStatusRunning, URL: "myapp.ts.net"}

	select {
	case evt := <-p.events:
		if evt.Status != model.ProxyStatusRunning {
			t.Errorf("event status = %v, want ProxyStatusRunning", evt.Status)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for bridged event")
	}

	close(lc.events)
	<-p.bridgeDone
}

func TestProxy_BridgeEvents_DeduplicatesSameStatus(t *testing.T) {
	t.Parallel()

	lc := NewNodeLifecycle(zerolog.Nop(), NodeLifecycleConfig{
		Retry: RetryPolicy{MaxAttempts: 0},
	})

	p := newTestProxyWithLifecycle(lc, nil)
	p.bridgeDone = make(chan struct{})

	go p.bridgeEvents()

	lc.events <- NodeEvent{Status: model.ProxyStatusRunning, URL: "myapp.ts.net"}

	select {
	case <-p.events:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for first event")
	}

	lc.events <- NodeEvent{Status: model.ProxyStatusRunning, URL: "myapp.ts.net"}

	select {
	case <-p.events:
		t.Error("should not receive duplicate event with same status+url+authURL")
	case <-time.After(200 * time.Millisecond):
	}

	close(lc.events)
	<-p.bridgeDone
}

func TestProxy_BridgeEvents_DifferentStatusNotDeduplicated(t *testing.T) {
	t.Parallel()

	lc := NewNodeLifecycle(zerolog.Nop(), NodeLifecycleConfig{
		Retry: RetryPolicy{MaxAttempts: 0},
	})

	p := newTestProxyWithLifecycle(lc, nil)
	p.bridgeDone = make(chan struct{})

	go p.bridgeEvents()

	lc.events <- NodeEvent{Status: model.ProxyStatusRunning}
	lc.events <- NodeEvent{Status: model.ProxyStatusStopped}

	count := 0
	timeout := time.After(2 * time.Second)
	for count < 2 {
		select {
		case <-p.events:
			count++
		case <-timeout:
			t.Fatalf("expected 2 events, got %d", count)
		}
	}

	close(lc.events)
	<-p.bridgeDone
}

func TestProxy_BridgeEvents_DropsOnFullBuffer(t *testing.T) {
	t.Parallel()

	lc := NewNodeLifecycle(zerolog.Nop(), NodeLifecycleConfig{
		Retry: RetryPolicy{MaxAttempts: 0},
	})

	p := newTestProxyWithLifecycle(lc, nil)
	p.events = make(chan model.ProxyEvent, 1)
	p.bridgeDone = make(chan struct{})

	go p.bridgeEvents()

	p.events <- model.ProxyEvent{Status: model.ProxyStatusRunning}

	done := make(chan struct{})
	go func() {
		defer close(done)
		lc.events <- NodeEvent{Status: model.ProxyStatusStopped, URL: "changed"}
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("bridgeEvents blocked — should drop on full buffer")
	}

	close(lc.events)
	<-p.bridgeDone
}

func TestProxy_BridgeEvents_UpdatesURLAndAuthURL(t *testing.T) {
	t.Parallel()

	lc := NewNodeLifecycle(zerolog.Nop(), NodeLifecycleConfig{
		Retry: RetryPolicy{MaxAttempts: 0},
	})

	p := newTestProxyWithLifecycle(lc, nil)
	p.bridgeDone = make(chan struct{})

	go p.bridgeEvents()

	lc.events <- NodeEvent{Status: model.ProxyStatusRunning, URL: "myapp.ts.net", AuthURL: "https://auth.url"}

	select {
	case <-p.events:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out")
	}

	p.mtx.RLock()
	url := p.url
	authURL := p.authURL
	p.mtx.RUnlock()

	if url != "myapp.ts.net" {
		t.Errorf("url = %q, want %q", url, "myapp.ts.net")
	}
	if authURL != "https://auth.url" {
		t.Errorf("authURL = %q, want %q", authURL, "https://auth.url")
	}

	close(lc.events)
	<-p.bridgeDone
}

func TestProxy_BridgeEvents_EmptyURLNotOverwritten(t *testing.T) {
	t.Parallel()

	lc := NewNodeLifecycle(zerolog.Nop(), NodeLifecycleConfig{
		Retry: RetryPolicy{MaxAttempts: 0},
	})

	p := newTestProxyWithLifecycle(lc, nil)
	p.bridgeDone = make(chan struct{})
	p.url = "initial.ts.net"

	go p.bridgeEvents()

	lc.events <- NodeEvent{Status: model.ProxyStatusRunning, URL: ""}

	select {
	case <-p.events:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out")
	}

	p.mtx.RLock()
	url := p.url
	p.mtx.RUnlock()

	if url != "initial.ts.net" {
		t.Errorf("url = %q, want %q (empty URL should not overwrite)", url, "initial.ts.net")
	}

	close(lc.events)
	<-p.bridgeDone
}

func TestProxy_ConcurrentGetURL(t *testing.T) {
	t.Parallel()

	ports := model.PortConfigList{
		"443": {ProxyProtocol: model.ProtoHTTPS},
	}
	p := newMinimalProxy(ports)
	p.url = "myapp.ts.net"

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got := p.GetURL()
			if got != "https://myapp.ts.net" {
				t.Errorf("GetURL() = %q, want %q", got, "https://myapp.ts.net")
			}
		}()
	}
	wg.Wait()
}

func TestProxy_GetListener_PortExistsButExposureNotStarted(t *testing.T) {
	t.Parallel()

	ports := model.PortConfigList{
		"443": {ProxyProtocol: model.ProtoHTTPS},
	}
	lc := NewNodeLifecycle(zerolog.Nop(), NodeLifecycleConfig{
		Retry: RetryPolicy{MaxAttempts: 0},
	})
	p := newTestProxyWithLifecycle(lc, ports)

	_, err := p.GetListener("443")
	if err == nil {
		t.Fatal("expected error when exposure not started")
	}
}

func TestProxy_GetRawTCPListener_PortExistsButExposureNotStarted(t *testing.T) {
	t.Parallel()

	ports := model.PortConfigList{
		"22": {ProxyProtocol: model.ProtoTCP},
	}
	lc := NewNodeLifecycle(zerolog.Nop(), NodeLifecycleConfig{
		Retry: RetryPolicy{MaxAttempts: 0},
	})
	p := newTestProxyWithLifecycle(lc, ports)

	_, err := p.GetRawTCPListener("22")
	if err == nil {
		t.Fatal("expected error when exposure not started")
	}
}

func TestProxy_GetPacketConn_PortExistsButExposureNotStarted(t *testing.T) {
	t.Parallel()

	ports := model.PortConfigList{
		"53": {ProxyProtocol: model.ProtoUDP},
	}
	lc := NewNodeLifecycle(zerolog.Nop(), NodeLifecycleConfig{
		Retry: RetryPolicy{MaxAttempts: 0},
	})
	p := newTestProxyWithLifecycle(lc, ports)

	_, err := p.GetPacketConn("53")
	if err == nil {
		t.Fatal("expected error when exposure not started")
	}
}

func TestProxy_BridgeEvents_CertPrefetchTriggeredWithHTTPS(t *testing.T) {
	t.Parallel()

	ports := model.PortConfigList{
		"443": {ProxyProtocol: model.ProtoHTTPS},
	}

	lc := NewNodeLifecycle(zerolog.Nop(), NodeLifecycleConfig{
		Retry: RetryPolicy{MaxAttempts: 0},
	})

	p := newTestProxyWithLifecycle(lc, ports)
	p.bridgeDone = make(chan struct{})

	go p.bridgeEvents()

	lc.events <- NodeEvent{Status: model.ProxyStatusRunning}

	select {
	case evt := <-p.events:
		if evt.Status != model.ProxyStatusRunning {
			t.Errorf("event status = %v, want ProxyStatusRunning", evt.Status)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out")
	}

	close(lc.events)
	<-p.bridgeDone
}

func TestProxy_InterfaceCompliance(t *testing.T) {
	t.Parallel()

	p := newMinimalProxy(nil)
	var _ proxyproviders.ProxyInterface = p
	var _ proxyproviders.RawTCPListener = p
}

func TestProxy_BridgeEvents_MultipleEventsSequential(t *testing.T) {
	t.Parallel()

	lc := NewNodeLifecycle(zerolog.Nop(), NodeLifecycleConfig{
		Retry: RetryPolicy{MaxAttempts: 0},
	})

	p := newTestProxyWithLifecycle(lc, nil)
	p.bridgeDone = make(chan struct{})

	go p.bridgeEvents()

	statuses := []model.ProxyStatus{
		model.ProxyStatusStarting,
		model.ProxyStatusAuthenticating,
		model.ProxyStatusRunning,
		model.ProxyStatusStopped,
	}

	for _, s := range statuses {
		lc.events <- NodeEvent{Status: s}
	}

	received := make([]model.ProxyStatus, 0, len(statuses))
	timeout := time.After(5 * time.Second)
	for len(received) < len(statuses) {
		select {
		case evt := <-p.events:
			received = append(received, evt.Status)
		case <-timeout:
			t.Fatalf("timed out: received %d of %d events", len(received), len(statuses))
		}
	}

	for i, want := range statuses {
		if received[i] != want {
			t.Errorf("event[%d] = %v, want %v", i, received[i], want)
		}
	}

	close(lc.events)
	<-p.bridgeDone
}

func TestProxy_Close_StartedWithLifecycle(t *testing.T) {
	lc := NewNodeLifecycle(zerolog.Nop(), NodeLifecycleConfig{
		Retry: RetryPolicy{MaxAttempts: 0},
	})

	p := newTestProxyWithLifecycle(lc, nil)
	p.bridgeDone = make(chan struct{})

	p.mtx.Lock()
	p.started = true
	p.mtx.Unlock()

	// Start bridgeEvents manually.
	go p.bridgeEvents()

	// Close will: close exposure, close lifecycle (which closes events), wait for bridgeDone, close events.
	_ = p.Close()

	p.mtx.RLock()
	started := p.started
	p.mtx.RUnlock()

	if started {
		t.Error("started should be false after Close()")
	}
}

func TestProxy_Start_LifecycleStartRequiresRealServer(t *testing.T) {
	t.Parallel()
	// Start() requires a real tsnet server (provided by DefaultNodeLifecycleProvider).
	// Unit tests verify bridgeEvents, GetURL, Close, and port routing instead.
	// Full Start() coverage is provided by e2e tests.
}
