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
	"sync/atomic"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/almeidapaulopt/tsdproxy/internal/dnsproviders"
	"github.com/almeidapaulopt/tsdproxy/internal/model"
	"github.com/almeidapaulopt/tsdproxy/internal/proxyproviders"
	"github.com/almeidapaulopt/tsdproxy/internal/targetproviders"
	"github.com/almeidapaulopt/tsdproxy/internal/tlsproviders"
)

type mockProxyInterface struct {
	startFn        func(ctx context.Context) error
	closeFn        func() error
	listenerFn     func(port string) (net.Listener, error)
	eventsCh       chan model.ProxyEvent
	url            string
	authURL        string
	listeners      []net.Listener
	startCallCount atomic.Int64
	closeCallCount atomic.Int64
	closeOnce      sync.Once
	listenersMu    sync.Mutex
}

func newMockProxyInterface() *mockProxyInterface {
	return &mockProxyInterface{
		url:      "https://test.tailnet.ts.net",
		eventsCh: make(chan model.ProxyEvent),
	}
}

func (m *mockProxyInterface) Start(ctx context.Context) error {
	m.startCallCount.Add(1)
	if m.startFn != nil {
		return m.startFn(ctx)
	}
	return nil
}

func (m *mockProxyInterface) Close() error {
	m.closeCallCount.Add(1)
	m.closeOnce.Do(func() {
		close(m.eventsCh)
		m.listenersMu.Lock()
		for _, ln := range m.listeners {
			_ = ln.Close()
		}
		m.listeners = nil
		m.listenersMu.Unlock()
		if m.closeFn != nil {
			_ = m.closeFn()
		}
	})
	return nil
}

func (m *mockProxyInterface) GetListener(port string) (net.Listener, error) {
	if m.listenerFn != nil {
		return m.listenerFn(port)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	m.listenersMu.Lock()
	m.listeners = append(m.listeners, ln)
	m.listenersMu.Unlock()
	return ln, nil
}

func (m *mockProxyInterface) GetPacketConn(_ string) (net.PacketConn, error) {
	return nil, errors.New("not implemented")
}

func (m *mockProxyInterface) GetURL() string                     { return m.url }
func (m *mockProxyInterface) GetAuthURL() string                 { return m.authURL }
func (m *mockProxyInterface) WatchEvents() chan model.ProxyEvent { return m.eventsCh }
func (m *mockProxyInterface) Whois(_ *http.Request) model.Whois  { return model.Whois{} }

var _ proxyproviders.ProxyInterface = (*mockProxyInterface)(nil)

type mockProxyProvider struct {
	newProxyErr    error
	startFn        func(ctx context.Context) error
	beforeNewProxy func()
	ifaces         []*mockProxyInterface
	mu             sync.Mutex
}

func (p *mockProxyProvider) ResolveAuthKey(_ *model.Config) (string, error) {
	return "test-authkey", nil
}

func (p *mockProxyProvider) NewProxy(_ *model.Config) (proxyproviders.ProxyInterface, error) {
	if p.beforeNewProxy != nil {
		p.beforeNewProxy()
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.newProxyErr != nil {
		return nil, p.newProxyErr
	}
	m := newMockProxyInterface()
	if p.startFn != nil {
		m.startFn = p.startFn
	}
	p.ifaces = append(p.ifaces, m)
	return m, nil
}

var _ proxyproviders.Provider = (*mockProxyProvider)(nil)

type mockTargetProvider struct {
	configs       map[string]*model.Config
	deleteProxyFn func(id string) error
	addTargetFn   func(id string) (*model.Config, error)
	deleteCalls   atomic.Int64
}

func (m *mockTargetProvider) WatchEvents(_ context.Context, _ chan targetproviders.TargetEvent, _ chan error) {
}

func (m *mockTargetProvider) GetDefaultProxyProviderName() string { return "default" }
func (m *mockTargetProvider) Close()                              {}

func (m *mockTargetProvider) AddTarget(id string) (*model.Config, error) {
	if m.addTargetFn != nil {
		return m.addTargetFn(id)
	}
	if cfg, ok := m.configs[id]; ok {
		return cfg, nil
	}
	return nil, fmt.Errorf("target %s not found", id)
}

func (m *mockTargetProvider) DeleteProxy(id string) error {
	m.deleteCalls.Add(1)
	if m.deleteProxyFn != nil {
		return m.deleteProxyFn(id)
	}
	return nil
}

func (m *mockTargetProvider) ReResolve(id string) (*model.Config, error) {
	return m.AddTarget(id)
}

var _ targetproviders.TargetProvider = (*mockTargetProvider)(nil)

type trackingDNSProvider struct {
	name          string
	createCount   atomic.Int64
	deleteCount   atomic.Int64
	validateCount atomic.Int64
}

func (t *trackingDNSProvider) Name() string { return t.name }
func (t *trackingDNSProvider) CreateRecord(_ context.Context, _, _, _ string) error {
	t.createCount.Add(1)
	return nil
}

func (t *trackingDNSProvider) DeleteRecord(_ context.Context, _, _ string) error {
	t.deleteCount.Add(1)
	return nil
}

func (t *trackingDNSProvider) ValidateRecord(_ context.Context, _, _, _ string) (bool, error) {
	t.validateCount.Add(1)
	return true, nil
}

func newConcurrentTestPM(t *testing.T) *ProxyManager {
	t.Helper()
	cfg := newTestConfig(t)
	pm := newTestProxyManager(cfg)
	pm.dnsLifecycle = dnsproviders.NewLifecycleManager(true)
	pm.tlsLifecycle = tlsproviders.NewTLSLifecycleManager(true)
	pm.ProxyProviders["default"] = &mockProxyProvider{}
	return pm
}

func newTestProxyConfig(hostname, targetID string) *model.Config {
	targetURL, _ := url.Parse("http://127.0.0.1:18080")
	pc := model.PortConfig{
		ProxyProtocol: model.ProtoHTTPS,
		ProxyPort:     443,
	}
	pc.AddTarget(targetURL)

	return &model.Config{
		Hostname:           hostname,
		TargetID:           targetID,
		TargetProvider:     "mock",
		ProxyProvider:      "default",
		HealthCheckEnabled: false,
		Ports: map[string]model.PortConfig{
			"1": pc,
		},
	}
}

// newPrebuiltProxy creates a Proxy with a real context and mock provider for
// direct insertion into pm.Proxies. The caller MUST call cleanupProxy(p) to
// cancel the context and release resources (leaking the context goroutine
// otherwise).
func newPrebuiltProxy(hostname, targetID string) *Proxy {
	ctx, cancel := context.WithCancel(context.Background())
	mpi := newMockProxyInterface()
	return &Proxy{
		Config: &model.Config{
			Hostname:           hostname,
			TargetID:           targetID,
			HealthCheckEnabled: false,
			Ports:              make(model.PortConfigList),
		},
		ctx:           ctx,
		cancel:        cancel,
		providerProxy: mpi,
		ports:         make(map[string]portHandler),
		log:           zerolog.Nop(),
	}
}

func newPrebuiltProxyWithDNS(hostname, targetID, domain string, dnsProv dnsproviders.Provider) *Proxy {
	p := newPrebuiltProxy(hostname, targetID)
	p.Config.Domain = domain
	p.dnsProvider = dnsProv
	p.tlsProvider = &mockTLSProvider{name: "mock-tls"}
	return p
}

func cleanupProxy(t *testing.T, p *Proxy) {
	t.Helper()
	if p == nil {
		return
	}
	p.cancelCtx()
	p.Close()
}

// ============================================================================
// Bug 1: Same-hostname replacement race in eventStop.
// eventStop deletes from the map before acquiring hostMu; a concurrent
// restartProxyLocked can insert a new proxy whose resources get destroyed.
// ============================================================================

func TestBug1_EventStop_MustNotDeleteNewProxyDNSResources(t *testing.T) {
	pm := newConcurrentTestPM(t)

	dnsProv := &trackingDNSProvider{name: "tracking"}
	proxyA := newPrebuiltProxyWithDNS("myapp", "containerA", "app.example.com", dnsProv)
	pm.Proxies["myapp"] = proxyA
	pm.targetIndex["containerA"] = "myapp"

	deleteReached := make(chan struct{})
	deleteContinue := make(chan struct{})
	stopProvider := &mockTargetProvider{
		deleteProxyFn: func(_ string) error {
			close(deleteReached)
			<-deleteContinue
			return nil
		},
	}

	stopDone := make(chan struct{})
	go func() {
		defer close(stopDone)
		pm.HandleProxyEvent(targetproviders.TargetEvent{
			ID:             "containerA",
			Action:         targetproviders.ActionStopProxy,
			TargetProvider: stopProvider,
		})
	}()

	<-deleteReached

	// With the fix, eventStop finds the hostname but does NOT delete from
	// the map until after DeleteProxy returns and hostLock is acquired.
	// So proxy A should still exist at this point.
	_, exists := pm.GetProxy("myapp")
	require.True(t, exists, "proxy A should still exist while DeleteProxy is blocking")

	cfgB := newTestProxyConfig("myapp", "containerB")
	startDone := make(chan error, 1)
	go func() { startDone <- pm.restartProxyLocked("myapp", cfgB) }()
	require.NoError(t, <-startDone)

	proxyB, ok := pm.GetProxy("myapp")
	require.True(t, ok)
	require.Equal(t, "containerB", proxyB.Config.TargetID)

	deletesBefore := dnsProv.deleteCount.Load()

	close(deleteContinue)
	<-stopDone

	deletesAfter := dnsProv.deleteCount.Load()
	require.Equal(t, deletesBefore, deletesAfter,
		"BUG 1: eventStop cleaned up DNS for hostname \"myapp\" after proxy B was already "+
			"inserted — eventStop deletes from map before acquiring hostMu")

	cleanupProxy(t, proxyB)
}

// ============================================================================
// Bug 2: Stop blocked behind a hung start.
// HandleProxyEvent holds the target mutex across blocking p.Start(), so a stop
// event for the same target can never proceed.
// ============================================================================

func TestBug2_Stop_MustNotBeBlockedByHungStart(t *testing.T) {
	pm := newConcurrentTestPM(t)

	startReached := make(chan struct{})
	startContinue := make(chan struct{})
	var startOnce sync.Once

	blockingProvider := &mockProxyProvider{
		startFn: func(ctx context.Context) error {
			startOnce.Do(func() { close(startReached) })
			select {
			case <-startContinue:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		},
	}
	pm.ProxyProviders["default"] = blockingProvider

	t.Cleanup(func() {
		select {
		case <-startContinue:
		default:
			close(startContinue)
		}
		time.Sleep(200 * time.Millisecond)
		if p, ok := pm.GetProxy("hungproxy"); ok {
			cleanupProxy(t, p)
		}
	})

	cfgX := newTestProxyConfig("hungproxy", "containerX")
	startProvider := &mockTargetProvider{
		configs: map[string]*model.Config{"containerX": cfgX},
	}

	go func() {
		pm.HandleProxyEvent(targetproviders.TargetEvent{
			ID:             "containerX",
			Action:         targetproviders.ActionStartProxy,
			TargetProvider: startProvider,
		})
	}()

	<-startReached

	stopProvider := &mockTargetProvider{}
	stopDone := make(chan struct{})
	go func() {
		defer close(stopDone)
		pm.HandleProxyEvent(targetproviders.TargetEvent{
			ID:             "containerX",
			Action:         targetproviders.ActionStopProxy,
			TargetProvider: stopProvider,
		})
	}()

	select {
	case <-stopDone:
	case <-time.After(2 * time.Second):
		t.Fatalf("BUG 2: stop event blocked for 2s behind hung Start() — " +
			"HandleProxyEvent holds the target mutex across blocking p.Start()")
	}
}

// ============================================================================
// Bug 3: Dashboard actions bypass event serialization.
// RestartProxy/PauseProxy/ResumeProxy must acquire the target lock and
// re-check map identity so a concurrent stop event can't interleave.
// ============================================================================

func TestBug3_RestartProxy_AcquiresTargetLock(t *testing.T) {
	pm := newConcurrentTestPM(t)

	cfgA := newTestProxyConfig("dashproxy", "containerA")
	require.NoError(t, pm.restartProxyLocked("dashproxy", cfgA))

	t.Cleanup(func() {
		if p, ok := pm.GetProxy("dashproxy"); ok {
			cleanupProxy(t, p)
		}
	})

	pm.targetLocks.Lock("containerA")

	restartDone := make(chan error, 1)
	go func() {
		restartDone <- pm.RestartProxy("dashproxy")
	}()

	select {
	case <-restartDone:
		t.Fatal("RestartProxy should block on target lock")
	case <-time.After(100 * time.Millisecond):
	}

	pm.targetLocks.Unlock("containerA")

	select {
	case err := <-restartDone:
		_ = err
	case <-time.After(2 * time.Second):
		t.Fatal("RestartProxy blocked after target lock released")
	}
}

func TestBug3_RestartProxy_ConcurrentStopDoesNotResurrect(t *testing.T) {
	pm := newConcurrentTestPM(t)

	cfgA := newTestProxyConfig("dashproxy", "containerA")
	require.NoError(t, pm.restartProxyLocked("dashproxy", cfgA))

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		pm.HandleProxyEvent(targetproviders.TargetEvent{
			ID:             "containerA",
			Action:         targetproviders.ActionStopProxy,
			TargetProvider: &mockTargetProvider{},
		})
	}()

	go func() {
		defer wg.Done()
		_ = pm.RestartProxy("dashproxy")
	}()

	wg.Wait()

	_, exists := pm.GetProxy("dashproxy")
	require.False(t, exists,
		"BUG 3: proxy survived concurrent restart+stop — "+
			"RestartProxy must serialize with stop events via target lock")

	if p, ok := pm.GetProxy("dashproxy"); ok {
		cleanupProxy(t, p)
	}
}

// ============================================================================
// Bug 4a: hostLocks leak after eventStop.
// Old sync.Map never cleaned up entries. keyedLocks should auto-clean.
// ============================================================================

func TestBug4a_HostLocks_MustNotLeakAfterEventStop(t *testing.T) {
	pm := newConcurrentTestPM(t)

	const numProxies = 20
	for i := range numProxies {
		hostname := fmt.Sprintf("ephemeral-%d", i)
		targetID := fmt.Sprintf("container-%d", i)
		cfg := newTestProxyConfig(hostname, targetID)

		require.NoError(t, pm.restartProxyLocked(hostname, cfg))

		stopProvider := &mockTargetProvider{}
		pm.HandleProxyEvent(targetproviders.TargetEvent{
			ID:             targetID,
			Action:         targetproviders.ActionStopProxy,
			TargetProvider: stopProvider,
		})
	}

	require.Equal(t, 0, len(pm.Proxies), "precondition: all proxies removed")

	require.Equal(t, 0, pm.hostLocks.count(),
		"BUG 4a: %d hostLock entries leaked — keyedLocks should auto-clean", pm.hostLocks.count())
}

// ============================================================================
// Bug 4b: keyedLocks must serialize concurrent callers.
// Old sync.Map Delete handed new mutexes to subsequent callers, breaking
// serialization. keyedLocks uses ref-counting to prevent this.
// ============================================================================

func TestBug4b_HostLocks_ProvideCorrectSerialization(t *testing.T) {
	pm := newConcurrentTestPM(t)

	const numGoroutines = 10
	var concurrent atomic.Int64
	var maxConcurrent atomic.Int64

	var wg sync.WaitGroup
	for range numGoroutines {
		wg.Add(1)
		go func() {
			defer wg.Done()
			pm.hostLocks.Lock("serialkey")
			defer pm.hostLocks.Unlock("serialkey")

			c := concurrent.Add(1)
			for {
				old := maxConcurrent.Load()
				if c <= old || maxConcurrent.CompareAndSwap(old, c) {
					break
				}
			}
			time.Sleep(2 * time.Millisecond)
			concurrent.Add(-1)
		}()
	}
	wg.Wait()

	require.Equal(t, int64(1), maxConcurrent.Load(),
		"BUG 4b: hostLocks failed to serialize — max concurrent holders was %d",
		maxConcurrent.Load())

	require.Equal(t, 0, pm.hostLocks.count(),
		"keyedLocks should auto-clean after all holders release")
}

// ============================================================================
// Bug 5: StopAllProxies can miss in-flight starts.
// StopAllProxies snapshots the map once; proxy contexts are not children of
// pm.ctx, so a start that inserts after the snapshot survives shutdown.
// ============================================================================

func TestBug5_StopAllProxies_MustCatchInFlightStart(t *testing.T) {
	pm := newConcurrentTestPM(t)

	cfgLate := newTestProxyConfig("lateproxy", "containerLate")

	insertContinue := make(chan struct{})
	startDone := make(chan struct{})
	go func() {
		<-insertContinue
		_ = pm.restartProxyLocked("lateproxy", cfgLate)
		close(startDone)
	}()

	pm.StopAllProxies()

	close(insertContinue)
	<-startDone

	_, exists := pm.GetProxy("lateproxy")
	require.False(t, exists,
		"BUG 5: proxy inserted after StopAllProxies snapshot survives shutdown")

	if p, ok := pm.GetProxy("lateproxy"); ok {
		cleanupProxy(t, p)
	}
}

func TestBug5_ProxyContext_MustBeChildOfManagerCtx(t *testing.T) {
	pm := newConcurrentTestPM(t)

	cfg := newTestProxyConfig("ctxtest", "containerC")
	require.NoError(t, pm.restartProxyLocked("ctxtest", cfg))

	p, ok := pm.GetProxy("ctxtest")
	require.True(t, ok)

	t.Cleanup(func() { cleanupProxy(t, p) })

	pm.cancel()

	select {
	case <-p.ctx.Done():
	case <-time.After(200 * time.Millisecond):
		t.Fatal("BUG 5: proxy context survives pm.ctx cancellation — " +
			"proxy contexts use context.WithCancel(context.Background()) " +
			"instead of context.WithCancel(pm.ctx)")
	}
}

// ============================================================================
// Bug 6: HandleProxyEvent Start() failure teardown can hit the wrong proxy.
// After Start() returns an error, HandleProxyEvent calls closeProxyIfStillCurrent
// which checks pointer identity before teardown. Without this check,
// closeAndRemoveProxy(hostname) would destroy whatever proxy currently holds
// the hostname — including a concurrent replacement inserted after Start() failed.
// ============================================================================

func TestBug6_CloseProxyIfStillCurrent_PreservesReplacement(t *testing.T) {
	pm := newConcurrentTestPM(t)

	p1 := newPrebuiltProxyWithDNS("shared", "containerA", "app.example.com",
		&trackingDNSProvider{name: "tracking"})
	p2 := newPrebuiltProxyWithDNS("shared", "containerB", "app.example.com",
		&trackingDNSProvider{name: "tracking2"})

	t.Cleanup(func() {
		cleanupProxy(t, p1)
		cleanupProxy(t, p2)
	})

	pm.proxyMu.Lock()
	pm.Proxies["shared"] = p2
	pm.targetIndex["containerB"] = "shared"
	pm.proxyMu.Unlock()

	p2CloseBefore := p2.providerProxy.(*mockProxyInterface).closeCallCount.Load()

	pm.closeProxyIfStillCurrent(p1)

	p2CloseAfter := p2.providerProxy.(*mockProxyInterface).closeCallCount.Load()
	require.Equal(t, p2CloseBefore, p2CloseAfter,
		"BUG 6: closeProxyIfStillCurrent tore down P2 when called with stale pointer P1")

	current, ok := pm.GetProxy("shared")
	require.True(t, ok, "P2 must remain in the map")
	require.Equal(t, p2, current, "P2 must be the current proxy")
	require.Equal(t, "containerB", current.Config.TargetID)
}

func TestBug6_CloseProxyIfStillCurrent_TearsDownMatch(t *testing.T) {
	pm := newConcurrentTestPM(t)

	p1 := newPrebuiltProxyWithDNS("shared", "containerA", "app.example.com",
		&trackingDNSProvider{name: "tracking"})

	t.Cleanup(func() { cleanupProxy(t, p1) })

	pm.proxyMu.Lock()
	pm.Proxies["shared"] = p1
	pm.targetIndex["containerA"] = "shared"
	pm.proxyMu.Unlock()

	pm.closeProxyIfStillCurrent(p1)

	_, ok := pm.GetProxy("shared")
	require.False(t, ok, "P1 should be removed when it is still current")
}

// ============================================================================
// Bug 7: StopAllProxies returns before in-flight HandleProxyEvent goroutines
// complete. dispatchProxyEvent tracks goroutines via eventHandlerWg; without
// that tracking, the process could exit mid-cleanup, losing resource teardown.
// ============================================================================

func TestBug7_StopAllProxies_WaitsForInFlightHandler(t *testing.T) {
	pm := newConcurrentTestPM(t)

	startReached := make(chan struct{})
	startContinue := make(chan struct{})
	var startOnce sync.Once
	blockingProvider := &mockProxyProvider{
		startFn: func(_ context.Context) error {
			startOnce.Do(func() { close(startReached) })
			// Deliberately do NOT listen on ctx.Done() — we want Start() to
			// stay blocked until the test explicitly releases it via
			// startContinue. This verifies that StopAllProxies blocks on
			// eventHandlerWg.Wait() rather than completing via ctx cancellation.
			<-startContinue
			return errors.New("simulated start failure")
		},
	}
	pm.ProxyProviders["default"] = blockingProvider

	cfg := newTestProxyConfig("blockedproxy", "containerBlocked")
	startProvider := &mockTargetProvider{configs: map[string]*model.Config{"containerBlocked": cfg}}

	pm.dispatchProxyEvent(targetproviders.TargetEvent{
		ID:             "containerBlocked",
		Action:         targetproviders.ActionStartProxy,
		TargetProvider: startProvider,
	})

	<-startReached

	stopDone := make(chan struct{})
	go func() {
		pm.StopAllProxies()
		close(stopDone)
	}()

	select {
	case <-stopDone:
		t.Fatal("BUG 7: StopAllProxies returned before in-flight HandleProxyEvent completed")
	case <-time.After(200 * time.Millisecond):
	}

	select {
	case <-startContinue:
	default:
		close(startContinue)
	}

	select {
	case <-stopDone:
	case <-time.After(5 * time.Second):
		t.Fatal("StopAllProxies didn't complete after handler finished")
	}
}
