// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package proxymanager

import (
	"context"
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"github.com/almeidapaulopt/tsdproxy/internal/config"
	"github.com/almeidapaulopt/tsdproxy/internal/dnsproviders"
	"github.com/almeidapaulopt/tsdproxy/internal/model"
	"github.com/almeidapaulopt/tsdproxy/internal/proxyproviders"
	"github.com/almeidapaulopt/tsdproxy/internal/targetproviders"
	"github.com/almeidapaulopt/tsdproxy/internal/tlsproviders"
)

// Stubs for proxymanager tests (not overlapping with proxy_test.go or providers_test.go stubs).

type stubProxyProv struct {
	resolveErr  error
	newProxyErr error
	newProxyRet proxyproviders.ProxyInterface
	resolvedKey string
	domainReq   bool
}

func (s *stubProxyProv) ResolveAuthKey(_ *model.Config) (string, error) {
	return s.resolvedKey, s.resolveErr
}

func (s *stubProxyProv) NewProxy(_ *model.Config) (proxyproviders.ProxyInterface, error) {
	if s.newProxyErr != nil {
		return nil, s.newProxyErr
	}
	if s.newProxyRet != nil {
		return s.newProxyRet, nil
	}
	return &stubProviderProxy{}, nil
}

func (s *stubProxyProv) IsDomainRequired() bool {
	return s.domainReq
}

type stubTargetProv struct {
	targetproviders.TargetProvider
	addTargetErr  error
	addTargetCfg  *model.Config
	deleteProxyFn func(string) error
	name          string
	defaultProv   string
}

func (s *stubTargetProv) GetDefaultProxyProviderName() string {
	if s.defaultProv != "" {
		return s.defaultProv
	}
	return "test"
}

func (s *stubTargetProv) AddTarget(_ string) (*model.Config, error) {
	return s.addTargetCfg, s.addTargetErr
}

func (s *stubTargetProv) DeleteProxy(id string) error {
	if s.deleteProxyFn != nil {
		return s.deleteProxyFn(id)
	}
	return nil
}

func (s *stubTargetProv) Close() {}

type stubDNSProv struct {
	dnsproviders.Provider
	name string
}

func (s *stubDNSProv) Name() string {
	if s.name != "" {
		return s.name
	}
	return "test-dns"
}

type stubTLSProv struct {
	tlsproviders.Provider
	name string
}

func (s *stubTLSProv) Name() string {
	if s.name != "" {
		return s.name
	}
	return "test-tls"
}

func newPMWithMocks() *ProxyManager {
	pm := newTestProxyManager()
	pm.TargetProviders["docker"] = &stubTargetProv{defaultProv: "tailscale"}
	pm.ProxyProviders["tailscale"] = &stubProxyProv{}
	return pm
}

// -- HandleProxyEvent ----------------------------------------------------------

func TestHandleProxyEvent_Start(t *testing.T) {
	t.Parallel()
	pm := newPMWithMocks()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()

	pm.ProxyProviders["tailscale"] = &stubProxyProv{
		newProxyRet: &stubProviderProxy{listener: l},
	}
	pm.TargetProviders["docker"] = &stubTargetProv{
		defaultProv: "tailscale",
		addTargetCfg: &model.Config{
			Hostname:       "test-app",
			TargetID:       "test-app-id",
			TargetProvider: "docker",
			Ports: map[string]model.PortConfig{
				"80": {ProxyProtocol: model.ProtoTCP},
			},
		},
	}
	event := targetproviders.TargetEvent{
		TargetProvider: pm.TargetProviders["docker"],
		ID:             "test-app-id",
		Action:         targetproviders.ActionStartProxy,
	}
	pm.HandleProxyEvent(event)

	pm.mtx.RLock()
	p, ok := pm.Proxies["test-app"]
	pm.mtx.RUnlock()
	if !ok {
		t.Fatal("expected proxy after ActionStartProxy")
	}
	p.Close()
}

func TestHandleProxyEvent_Start_AddTargetError(t *testing.T) {
	t.Parallel()
	pm := newPMWithMocks()
	pm.TargetProviders["docker"] = &stubTargetProv{addTargetErr: errors.New("no such container")}
	event := targetproviders.TargetEvent{
		TargetProvider: pm.TargetProviders["docker"],
		ID:             "broken",
		Action:         targetproviders.ActionStartProxy,
	}
	pm.HandleProxyEvent(event)

	pm.mtx.RLock()
	if len(pm.Proxies) != 0 {
		t.Fatal("expected no proxy after AddTarget error")
	}
	pm.mtx.RUnlock()
}

func TestHandleProxyEvent_Stop(t *testing.T) {
	t.Parallel()
	pm := newPMWithMocks()
	ctx, cancel := context.WithCancel(context.Background())
	pm.Proxies["my-proxy"] = &Proxy{
		Config:        &model.Config{TargetID: "my-id", Hostname: "my-proxy"},
		log:           zerolog.Nop(),
		ctx:           ctx,
		cancel:        cancel,
		statusHistory: make([]StatusTransition, 0, maxStatusHistory),
	}
	event := targetproviders.TargetEvent{
		TargetProvider: &stubTargetProv{},
		ID:             "my-id",
		Action:         targetproviders.ActionStopProxy,
	}
	pm.HandleProxyEvent(event)

	pm.mtx.RLock()
	_, ok := pm.Proxies["my-proxy"]
	pm.mtx.RUnlock()
	if ok {
		t.Fatal("expected proxy removed after ActionStopProxy")
	}
}

func TestHandleProxyEvent_Restart(t *testing.T) {
	t.Parallel()
	pm := newPMWithMocks()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()

	ctx, cancel := context.WithCancel(context.Background())
	pm.Proxies["restart-app"] = &Proxy{
		Config:        &model.Config{TargetID: "restart-id", Hostname: "restart-app"},
		log:           zerolog.Nop(),
		ctx:           ctx,
		cancel:        cancel,
		statusHistory: make([]StatusTransition, 0, maxStatusHistory),
	}
	pm.ProxyProviders["tailscale"] = &stubProxyProv{
		newProxyRet: &stubProviderProxy{listener: l},
	}
	pm.TargetProviders["docker"] = &stubTargetProv{
		defaultProv: "tailscale",
		addTargetCfg: &model.Config{
			Hostname:       "restart-app",
			TargetID:       "restart-id",
			TargetProvider: "docker",
			Ports:          map[string]model.PortConfig{"80": {ProxyProtocol: model.ProtoTCP}},
		},
	}
	event := targetproviders.TargetEvent{
		TargetProvider: pm.TargetProviders["docker"],
		ID:             "restart-id",
		Action:         targetproviders.ActionRestartProxy,
	}
	pm.HandleProxyEvent(event)

	pm.mtx.RLock()
	p, ok := pm.Proxies["restart-app"]
	pm.mtx.RUnlock()
	if !ok {
		t.Fatal("expected proxy after ActionRestartProxy")
	}
	p.Close()
}

func TestHandleProxyEvent_UnknownAction(t *testing.T) {
	t.Parallel()
	pm := newPMWithMocks()
	event := targetproviders.TargetEvent{
		TargetProvider: &stubTargetProv{},
		ID:             "x",
		Action:         targetproviders.ActionType(99),
	}
	pm.HandleProxyEvent(event)

	pm.mtx.RLock()
	if len(pm.Proxies) != 0 {
		t.Fatal("expected no proxy for unknown action")
	}
	pm.mtx.RUnlock()
}

// -- eventStop -----------------------------------------------------------------

func TestEventStop_RemovesProxyByTargetID(t *testing.T) {
	t.Parallel()
	pm := newPMWithMocks()
	ctxA, cancelA := context.WithCancel(context.Background())
	ctxB, cancelB := context.WithCancel(context.Background())
	pm.Proxies["a"] = &Proxy{
		Config:        &model.Config{TargetID: "target-a", Hostname: "a"},
		log:           zerolog.Nop(),
		ctx:           ctxA,
		cancel:        cancelA,
		statusHistory: make([]StatusTransition, 0, maxStatusHistory),
	}
	pm.Proxies["b"] = &Proxy{
		Config:        &model.Config{TargetID: "target-b", Hostname: "b"},
		log:           zerolog.Nop(),
		ctx:           ctxB,
		cancel:        cancelB,
		statusHistory: make([]StatusTransition, 0, maxStatusHistory),
	}
	pm.eventStop(targetproviders.TargetEvent{
		TargetProvider: &stubTargetProv{},
		ID:             "target-a",
	})

	pm.mtx.RLock()
	_, okA := pm.Proxies["a"]
	_, okB := pm.Proxies["b"]
	pm.mtx.RUnlock()
	if okA {
		t.Fatal("expected proxy 'a' removed")
	}
	if !okB {
		t.Fatal("expected proxy 'b' to remain")
	}
}

func TestEventStop_NonExistentTarget(t *testing.T) {
	t.Parallel()
	pm := newPMWithMocks()
	ctx, cancel := context.WithCancel(context.Background())
	pm.Proxies["a"] = &Proxy{
		Config:        &model.Config{TargetID: "target-a", Hostname: "a"},
		log:           zerolog.Nop(),
		ctx:           ctx,
		cancel:        cancel,
		statusHistory: make([]StatusTransition, 0, maxStatusHistory),
	}
	pm.eventStop(targetproviders.TargetEvent{
		TargetProvider: &stubTargetProv{deleteProxyFn: func(_ string) error {
			return errors.New("not found")
		}},
		ID: "nonexistent",
	})

	pm.mtx.RLock()
	_, ok := pm.Proxies["a"]
	pm.mtx.RUnlock()
	if !ok {
		t.Fatal("proxy 'a' should still exist")
	}
}

// -- closeAndRemoveProxy -------------------------------------------------------

func TestCloseAndRemoveProxy_Exists(t *testing.T) {
	t.Parallel()
	pm := newPMWithMocks()
	ctx, cancel := context.WithCancel(context.Background())
	pm.Proxies["test"] = &Proxy{
		Config:        &model.Config{Hostname: "test"},
		log:           zerolog.Nop(),
		ctx:           ctx,
		cancel:        cancel,
		statusHistory: make([]StatusTransition, 0, maxStatusHistory),
	}
	pm.closeAndRemoveProxy("test")

	pm.mtx.RLock()
	_, ok := pm.Proxies["test"]
	pm.mtx.RUnlock()
	if ok {
		t.Fatal("expected proxy removed")
	}
}

func TestCloseAndRemoveProxy_NotExists(_ *testing.T) {
	pm := newPMWithMocks()
	pm.closeAndRemoveProxy("ghost")
}

func TestCloseAndRemoveProxy_ProviderChangeWarning(t *testing.T) {
	t.Parallel()
	pm := newPMWithMocks()
	ctx, cancel := context.WithCancel(context.Background())
	pm.Proxies["test"] = &Proxy{
		Config:        &model.Config{Hostname: "test", ProxyProvider: "old-provider"},
		log:           zerolog.Nop(),
		ctx:           ctx,
		cancel:        cancel,
		statusHistory: make([]StatusTransition, 0, maxStatusHistory),
	}
	pm.closeAndRemoveProxy("test", "new-provider")

	pm.mtx.RLock()
	_, ok := pm.Proxies["test"]
	pm.mtx.RUnlock()
	if ok {
		t.Fatal("expected proxy removed")
	}
}

// -- StopAllProxies ------------------------------------------------------------

func TestStopAllProxies_Empty(_ *testing.T) {
	pm := newTestProxyManager()
	pm.StopAllProxies()
}

func TestStopAllProxies_WithProxies(t *testing.T) {
	t.Parallel()
	pm := newTestProxyManager()
	ctxA, cancelA := context.WithCancel(context.Background())
	ctxB, cancelB := context.WithCancel(context.Background())
	pm.Proxies["a"] = &Proxy{
		Config:        &model.Config{Hostname: "a"},
		log:           zerolog.Nop(),
		ctx:           ctxA,
		cancel:        cancelA,
		statusHistory: make([]StatusTransition, 0, maxStatusHistory),
	}
	pm.Proxies["b"] = &Proxy{
		Config:        &model.Config{Hostname: "b"},
		log:           zerolog.Nop(),
		ctx:           ctxB,
		cancel:        cancelB,
		statusHistory: make([]StatusTransition, 0, maxStatusHistory),
	}
	pm.StopAllProxies()

	pm.mtx.RLock()
	if len(pm.Proxies) != 0 {
		t.Fatal("expected all proxies removed")
	}
	pm.mtx.RUnlock()
}

// -- getProxyProvider ----------------------------------------------------------

func TestGetProxyProvider_ByProxyConfig(t *testing.T) {
	t.Parallel()
	pm := newTestProxyManager()
	prov := &stubProxyProv{}
	pm.ProxyProviders["myprov"] = prov
	p, err := pm.getProxyProvider(&model.Config{ProxyProvider: "myprov"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p != prov {
		t.Fatal("expected provider 'myprov'")
	}
}

func TestGetProxyProvider_ByProxyConfig_Lowercase(t *testing.T) {
	t.Parallel()
	pm := newTestProxyManager()
	prov := &stubProxyProv{}
	pm.ProxyProviders["myprov"] = prov
	p, err := pm.getProxyProvider(&model.Config{ProxyProvider: "MyProv"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p != prov {
		t.Fatal("expected provider 'myprov'")
	}
}

func TestGetProxyProvider_ByProxyConfig_NotFound(t *testing.T) {
	t.Parallel()
	pm := newTestProxyManager()
	_, err := pm.getProxyProvider(&model.Config{ProxyProvider: "missing"})
	if !errors.Is(err, ErrProxyProviderNotFound) {
		t.Fatalf("expected ErrProxyProviderNotFound, got %v", err)
	}
}

func TestGetProxyProvider_ByTargetDefault(t *testing.T) {
	t.Parallel()
	pm := newTestProxyManager()
	prov := &stubProxyProv{}
	pm.TargetProviders["docker"] = &stubTargetProv{defaultProv: "ts-default"}
	pm.ProxyProviders["ts-default"] = prov
	p, err := pm.getProxyProvider(&model.Config{TargetProvider: "docker"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p != prov {
		t.Fatal("expected provider 'ts-default'")
	}
}

func TestGetProxyProvider_ByTargetDefault_NotFound(t *testing.T) {
	t.Parallel()
	pm := newTestProxyManager()
	pm.TargetProviders["docker"] = &stubTargetProv{defaultProv: "nonexistent"}
	_, err := pm.getProxyProvider(&model.Config{TargetProvider: "docker"})
	if !errors.Is(err, ErrProxyProviderNotFound) {
		t.Fatalf("expected ErrProxyProviderNotFound, got %v", err)
	}
}

func TestGetProxyProvider_ByGlobalDefault(t *testing.T) {
	orig := config.Config.DefaultProxyProvider
	config.Config.DefaultProxyProvider = "global-ts"
	defer func() { config.Config.DefaultProxyProvider = orig }()

	pm := newTestProxyManager()
	prov := &stubProxyProv{}
	pm.TargetProviders["docker"] = &stubTargetProv{defaultProv: ""}
	pm.ProxyProviders["global-ts"] = prov
	p, err := pm.getProxyProvider(&model.Config{TargetProvider: "docker"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p != prov {
		t.Fatal("expected provider 'global-ts'")
	}
}

func TestGetProxyProvider_AllEmpty_ReturnsError(t *testing.T) {
	orig := config.Config.DefaultProxyProvider
	config.Config.DefaultProxyProvider = ""
	defer func() { config.Config.DefaultProxyProvider = orig }()

	pm := newTestProxyManager()
	pm.TargetProviders["docker"] = &stubTargetProv{defaultProv: ""}
	_, err := pm.getProxyProvider(&model.Config{TargetProvider: "docker"})
	if !errors.Is(err, ErrProxyProviderNotFound) {
		t.Fatalf("expected ErrProxyProviderNotFound, got %v", err)
	}
}

func TestGetProxyProvider_TargetProviderNotFound(t *testing.T) {
	t.Parallel()
	pm := newTestProxyManager()
	_, err := pm.getProxyProvider(&model.Config{TargetProvider: "nonexistent"})
	if !errors.Is(err, ErrTargetProviderNotFound) {
		t.Fatalf("expected ErrTargetProviderNotFound, got %v", err)
	}
}

// -- resolveDNSProviderLocked ---------------------------------------------------

func TestResolveDNSProviderLocked_ByProxyConfig(t *testing.T) {
	t.Parallel()
	pm := newTestProxyManager()
	pm.DNSProviders["my-dns"] = &stubDNSProv{name: "my-dns"}
	p, err := pm.resolveDNSProviderLocked(&model.Config{DNSProvider: "my-dns"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Name() != "my-dns" {
		t.Fatalf("got %s", p.Name())
	}
}

func TestResolveDNSProviderLocked_ByProxyConfig_NotFound(t *testing.T) {
	t.Parallel()
	pm := newTestProxyManager()
	_, err := pm.resolveDNSProviderLocked(&model.Config{DNSProvider: "missing"})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestResolveDNSProviderLocked_ByGlobalDefault(t *testing.T) {
	orig := config.Config.DefaultDNSProvider
	config.Config.DefaultDNSProvider = "global-dns"
	defer func() { config.Config.DefaultDNSProvider = orig }()

	pm := newTestProxyManager()
	pm.DNSProviders["global-dns"] = &stubDNSProv{name: "global-dns"}
	p, err := pm.resolveDNSProviderLocked(&model.Config{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Name() != "global-dns" {
		t.Fatalf("got %s", p.Name())
	}
}

func TestResolveDNSProviderLocked_GlobalDefaultNotFound(t *testing.T) {
	orig := config.Config.DefaultDNSProvider
	config.Config.DefaultDNSProvider = "missing"
	defer func() { config.Config.DefaultDNSProvider = orig }()

	pm := newTestProxyManager()
	_, err := pm.resolveDNSProviderLocked(&model.Config{})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestResolveDNSProviderLocked_NoneConfigured(t *testing.T) {
	orig := config.Config.DefaultDNSProvider
	config.Config.DefaultDNSProvider = ""
	defer func() { config.Config.DefaultDNSProvider = orig }()

	pm := newTestProxyManager()
	_, err := pm.resolveDNSProviderLocked(&model.Config{})
	if !errors.Is(err, ErrNoDNSProvider) {
		t.Fatalf("expected ErrNoDNSProvider, got %v", err)
	}
}

func TestResolveDNSProviderLocked_ProxyConfigTakesPrecedence(t *testing.T) {
	orig := config.Config.DefaultDNSProvider
	config.Config.DefaultDNSProvider = "global"
	defer func() { config.Config.DefaultDNSProvider = orig }()

	pm := newTestProxyManager()
	pm.DNSProviders["global"] = &stubDNSProv{name: "global"}
	pm.DNSProviders["per-proxy"] = &stubDNSProv{name: "per-proxy"}
	p, err := pm.resolveDNSProviderLocked(&model.Config{DNSProvider: "per-proxy"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Name() != "per-proxy" {
		t.Fatalf("got %s", p.Name())
	}
}

// -- resolveTLSProviderLocked ---------------------------------------------------

func TestResolveTLSProviderLocked_ByProxyConfig(t *testing.T) {
	t.Parallel()
	pm := newTestProxyManager()
	pm.TLSProviders["my-tls"] = &stubTLSProv{name: "my-tls"}
	p, err := pm.resolveTLSProviderLocked(&model.Config{TLSProvider: "my-tls"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Name() != "my-tls" {
		t.Fatalf("got %s", p.Name())
	}
}

func TestResolveTLSProviderLocked_TailscaleName(t *testing.T) {
	t.Parallel()
	pm := newTestProxyManager()
	p, err := pm.resolveTLSProviderLocked(&model.Config{TLSProvider: "tailscale"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Name() != "tailscale" {
		t.Fatalf("got %s", p.Name())
	}
}

func TestResolveTLSProviderLocked_TailscaleInConfig(t *testing.T) {
	orig := config.Config.TLSProviders
	config.Config.TLSProviders = map[string]*config.TLSProviderConfig{
		"ts": {Provider: "tailscale"},
	}
	defer func() { config.Config.TLSProviders = orig }()

	pm := newTestProxyManager()
	p, err := pm.resolveTLSProviderLocked(&model.Config{TLSProvider: "ts"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Name() != "tailscale" {
		t.Fatalf("got %s", p.Name())
	}
}

func TestResolveTLSProviderLocked_NotFound(t *testing.T) {
	t.Parallel()
	pm := newTestProxyManager()
	_, err := pm.resolveTLSProviderLocked(&model.Config{TLSProvider: "missing"})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestResolveTLSProviderLocked_NoneConfigured(t *testing.T) {
	orig := config.Config.DefaultTLSProvider
	config.Config.DefaultTLSProvider = ""
	defer func() { config.Config.DefaultTLSProvider = orig }()

	pm := newTestProxyManager()
	_, err := pm.resolveTLSProviderLocked(&model.Config{})
	if !errors.Is(err, ErrNoTLSProvider) {
		t.Fatalf("expected ErrNoTLSProvider, got %v", err)
	}
}

func TestResolveTLSProviderLocked_UsesDefault(t *testing.T) {
	orig := config.Config.DefaultTLSProvider
	config.Config.DefaultTLSProvider = "my-default"
	defer func() { config.Config.DefaultTLSProvider = orig }()

	pm := newTestProxyManager()
	pm.TLSProviders["my-default"] = &stubTLSProv{name: "my-default"}
	p, err := pm.resolveTLSProviderLocked(&model.Config{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Name() != "my-default" {
		t.Fatalf("got %s", p.Name())
	}
}

// -- extractHost ---------------------------------------------------------------

func TestExtractHost(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input    string
		expected string
	}{
		{"https://myapp.tailnet.ts.net:443", "myapp.tailnet.ts.net:443"},
		{"http://example.com", "example.com"},
		{"https://host.name/path", "host.name"},
		{"plain-string", "plain-string"},
		{"", ""},
		{"://invalid", "://invalid"},
	}
	for _, tc := range tests {
		got := extractHost(tc.input)
		if got != tc.expected {
			t.Errorf("extractHost(%q) = %q, want %q", tc.input, got, tc.expected)
		}
	}
}

// -- addTargetProvider / getTargetProvider -------------------------------------

func TestAddAndGetTargetProvider(t *testing.T) {
	t.Parallel()
	pm := newTestProxyManager()
	tp := &stubTargetProv{name: "docker"}
	pm.addTargetProvider(tp, "docker")
	got, ok := pm.getTargetProvider("docker")
	if !ok {
		t.Fatal("expected target provider to exist")
	}
	if got != tp {
		t.Fatal("expected same instance")
	}
}

func TestGetTargetProvider_NotFound(t *testing.T) {
	t.Parallel()
	pm := newTestProxyManager()
	_, ok := pm.getTargetProvider("nonexistent")
	if ok {
		t.Fatal("expected false")
	}
}

// -- getTargetLock (additional concurrency test) -------------------------------

func TestGetTargetLock_LocksExclusively(t *testing.T) {
	t.Parallel()
	pm := newTestProxyManager()
	m := pm.getTargetLock("lock-test")
	m.Lock()
	locked := make(chan bool)
	go func() {
		m.Lock()
		close(locked)
	}()
	select {
	case <-locked:
		t.Fatal("second Lock should block")
	case <-time.After(10 * time.Millisecond):
	}
	m.Unlock()
	select {
	case <-locked:
	case <-time.After(time.Second):
		t.Fatal("second Lock should have acquired after unlock")
	}
	m.Unlock()
}

// -- updateProxyCount / cleanupProxyMetrics ------------------------------------

func TestUpdateProxyCount(_ *testing.T) {
	pm := newTestProxyManager()
	pm.Proxies["a"] = &Proxy{Config: &model.Config{Hostname: "a"}}
	pm.Proxies["b"] = &Proxy{Config: &model.Config{Hostname: "b"}}
	pm.updateProxyCount()
}

func TestUpdateProxyCount_NilMetrics(t *testing.T) {
	t.Parallel()
	pm := newTestProxyManager()
	pm.metrics = nil
	pm.updateProxyCount()
}

func TestCleanupProxyMetrics(t *testing.T) {
	t.Parallel()
	pm := newTestProxyManager()
	pm.Proxies["test"] = &Proxy{Config: &model.Config{Hostname: "test"}}
	pm.cleanupProxyMetrics("test")
}

func TestCleanupProxyMetrics_NilMetrics(t *testing.T) {
	t.Parallel()
	pm := newTestProxyManager()
	pm.metrics = nil
	pm.cleanupProxyMetrics("test")
}

// -- getHostLock ---------------------------------------------------------------

func TestGetHostLock_SameHostname_ReturnsSameMutex(t *testing.T) {
	t.Parallel()
	pm := newTestProxyManager()
	m1 := pm.getHostLock("test-host")
	m2 := pm.getHostLock("test-host")
	if m1 != m2 {
		t.Fatal("expected same mutex for same hostname")
	}
}

func TestGetHostLock_DifferentHostnames_DifferentMutexes(t *testing.T) {
	t.Parallel()
	pm := newTestProxyManager()
	m1 := pm.getHostLock("host-1")
	m2 := pm.getHostLock("host-2")
	if m1 == m2 {
		t.Fatal("expected different mutexes")
	}
}

// -- StopAllProxies concurrency safety -----------------------------------------

func TestStopAllProxies_ConcurrentSafe(t *testing.T) {
	t.Parallel()
	pm := newTestProxyManager()
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			ctx, cancel := context.WithCancel(context.Background())
			name := string(rune('a' + n))
			pm.mtx.Lock()
			pm.Proxies[name] = &Proxy{
				Config:        &model.Config{Hostname: name},
				log:           zerolog.Nop(),
				ctx:           ctx,
				cancel:        cancel,
				statusHistory: make([]StatusTransition, 0, maxStatusHistory),
			}
			pm.mtx.Unlock()
		}(i)
	}
	wg.Wait()

	pm.StopAllProxies()

	pm.mtx.RLock()
	if len(pm.Proxies) != 0 {
		t.Fatalf("expected 0 proxies after StopAllProxies")
	}
	pm.mtx.RUnlock()
}

// -- addProxyProvider ----------------------------------------------------------

func TestAddProxyProvider_ConvertsToLowercase(t *testing.T) {
	t.Parallel()
	pm := newTestProxyManager()
	pm.addProxyProvider(&stubProxyProv{}, "MyProv")
	_, ok := pm.ProxyProviders["myprov"]
	if !ok {
		t.Fatal("expected provider stored as lowercase")
	}
}
