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
	"github.com/stretchr/testify/assert"

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

func newPMWithMocks(t *testing.T) *ProxyManager {
	t.Helper()
	pm := newTestProxyManager(newTestConfig(t))
	pm.TargetProviders["docker"] = &stubTargetProv{defaultProv: "tailscale"}
	pm.ProxyProviders["tailscale"] = &stubProxyProv{}
	return pm
}

// -- HandleProxyEvent ----------------------------------------------------------

func TestHandleProxyEvent_Start(t *testing.T) {
	t.Parallel()
	pm := newPMWithMocks(t)
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

	pm.proxyMu.RLock()
	p, ok := pm.Proxies["test-app"]
	pm.proxyMu.RUnlock()
	if !ok {
		t.Fatal("expected proxy after ActionStartProxy")
	}
	p.Close()
}

func TestHandleProxyEvent_Start_AddTargetError(t *testing.T) {
	t.Parallel()
	pm := newPMWithMocks(t)
	pm.TargetProviders["docker"] = &stubTargetProv{addTargetErr: errors.New("no such container")}
	event := targetproviders.TargetEvent{
		TargetProvider: pm.TargetProviders["docker"],
		ID:             "broken",
		Action:         targetproviders.ActionStartProxy,
	}
	pm.HandleProxyEvent(event)

	pm.proxyMu.RLock()
	if len(pm.Proxies) != 0 {
		t.Fatal("expected no proxy after AddTarget error")
	}
	pm.proxyMu.RUnlock()
}

func TestHandleProxyEvent_Stop(t *testing.T) {
	t.Parallel()
	pm := newPMWithMocks(t)
	ctx, cancel := context.WithCancel(context.Background())
	pm.Proxies["my-proxy"] = &Proxy{
		Config:        &model.Config{TargetID: "my-id", Hostname: "my-proxy"},
		log:           zerolog.Nop(),
		ctx:           ctx,
		cancel:        cancel,
		statusHistory: make([]StatusTransition, 0, maxStatusHistory),
	}
	pm.targetIndex["my-id"] = "my-proxy"
	event := targetproviders.TargetEvent{
		TargetProvider: &stubTargetProv{},
		ID:             "my-id",
		Action:         targetproviders.ActionStopProxy,
	}
	pm.HandleProxyEvent(event)

	pm.proxyMu.RLock()
	_, ok := pm.Proxies["my-proxy"]
	pm.proxyMu.RUnlock()
	if ok {
		t.Fatal("expected proxy removed after ActionStopProxy")
	}
}

func TestHandleProxyEvent_Restart(t *testing.T) {
	t.Parallel()
	pm := newPMWithMocks(t)
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
	pm.targetIndex["restart-id"] = "restart-app"
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

	pm.proxyMu.RLock()
	p, ok := pm.Proxies["restart-app"]
	pm.proxyMu.RUnlock()
	if !ok {
		t.Fatal("expected proxy after ActionRestartProxy")
	}
	p.Close()
}

func TestHandleProxyEvent_UnknownAction(t *testing.T) {
	t.Parallel()
	pm := newPMWithMocks(t)
	event := targetproviders.TargetEvent{
		TargetProvider: &stubTargetProv{},
		ID:             "x",
		Action:         targetproviders.ActionType(99),
	}
	pm.HandleProxyEvent(event)

	pm.proxyMu.RLock()
	if len(pm.Proxies) != 0 {
		t.Fatal("expected no proxy for unknown action")
	}
	pm.proxyMu.RUnlock()
}

// -- eventStop -----------------------------------------------------------------

func TestEventStop_RemovesProxyByTargetID(t *testing.T) {
	t.Parallel()
	pm := newPMWithMocks(t)
	ctxA, cancelA := context.WithCancel(context.Background())
	ctxB, cancelB := context.WithCancel(context.Background())
	pm.Proxies["a"] = &Proxy{
		Config:        &model.Config{TargetID: "target-a", Hostname: "a"},
		log:           zerolog.Nop(),
		ctx:           ctxA,
		cancel:        cancelA,
		statusHistory: make([]StatusTransition, 0, maxStatusHistory),
	}
	pm.targetIndex["target-a"] = "a"
	pm.Proxies["b"] = &Proxy{
		Config:        &model.Config{TargetID: "target-b", Hostname: "b"},
		log:           zerolog.Nop(),
		ctx:           ctxB,
		cancel:        cancelB,
		statusHistory: make([]StatusTransition, 0, maxStatusHistory),
	}
	pm.targetIndex["target-b"] = "b"
	pm.eventStop(targetproviders.TargetEvent{
		TargetProvider: &stubTargetProv{},
		ID:             "target-a",
	})

	pm.proxyMu.RLock()
	_, okA := pm.Proxies["a"]
	_, okB := pm.Proxies["b"]
	pm.proxyMu.RUnlock()
	if okA {
		t.Fatal("expected proxy 'a' removed")
	}
	if !okB {
		t.Fatal("expected proxy 'b' to remain")
	}
}

func TestEventStop_NonExistentTarget(t *testing.T) {
	t.Parallel()
	pm := newPMWithMocks(t)
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

	pm.proxyMu.RLock()
	_, ok := pm.Proxies["a"]
	pm.proxyMu.RUnlock()
	if !ok {
		t.Fatal("proxy 'a' should still exist")
	}
}

// -- closeAndRemoveProxy -------------------------------------------------------

func TestCloseAndRemoveProxy_Exists(t *testing.T) {
	t.Parallel()
	pm := newPMWithMocks(t)
	ctx, cancel := context.WithCancel(context.Background())
	pm.Proxies["test"] = &Proxy{
		Config:        &model.Config{Hostname: "test"},
		log:           zerolog.Nop(),
		ctx:           ctx,
		cancel:        cancel,
		statusHistory: make([]StatusTransition, 0, maxStatusHistory),
	}
	pm.closeAndRemoveProxy("test")

	pm.proxyMu.RLock()
	_, ok := pm.Proxies["test"]
	pm.proxyMu.RUnlock()
	if ok {
		t.Fatal("expected proxy removed")
	}
}

func TestCloseAndRemoveProxy_NotExists(t *testing.T) {
	pm := newPMWithMocks(t)
	pm.closeAndRemoveProxy("ghost")
}

func TestCloseAndRemoveProxy_ProviderChangeWarning(t *testing.T) {
	t.Parallel()
	pm := newPMWithMocks(t)
	ctx, cancel := context.WithCancel(context.Background())
	pm.Proxies["test"] = &Proxy{
		Config:        &model.Config{Hostname: "test", ProxyProvider: "old-provider"},
		log:           zerolog.Nop(),
		ctx:           ctx,
		cancel:        cancel,
		statusHistory: make([]StatusTransition, 0, maxStatusHistory),
	}
	pm.closeAndRemoveProxy("test", "new-provider")

	pm.proxyMu.RLock()
	_, ok := pm.Proxies["test"]
	pm.proxyMu.RUnlock()
	if ok {
		t.Fatal("expected proxy removed")
	}
}

// -- StopAllProxies ------------------------------------------------------------

func TestStopAllProxies_Empty(t *testing.T) {
	pm := newTestProxyManager(newTestConfig(t))
	pm.StopAllProxies()
}

func TestStopAllProxies_WithProxies(t *testing.T) {
	t.Parallel()
	pm := newTestProxyManager(newTestConfig(t))
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

	pm.proxyMu.RLock()
	if len(pm.Proxies) != 0 {
		t.Fatal("expected all proxies removed")
	}
	pm.proxyMu.RUnlock()
}

// -- getProxyProvider ----------------------------------------------------------

func TestGetProxyProvider_ByProxyConfig(t *testing.T) {
	t.Parallel()
	pm := newTestProxyManager(newTestConfig(t))
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
	pm := newTestProxyManager(newTestConfig(t))
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
	pm := newTestProxyManager(newTestConfig(t))
	_, err := pm.getProxyProvider(&model.Config{ProxyProvider: "missing"})
	if !errors.Is(err, ErrProxyProviderNotFound) {
		t.Fatalf("expected ErrProxyProviderNotFound, got %v", err)
	}
}

func TestGetProxyProvider_ByTargetDefault(t *testing.T) {
	t.Parallel()
	pm := newTestProxyManager(newTestConfig(t))
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
	pm := newTestProxyManager(newTestConfig(t))
	pm.TargetProviders["docker"] = &stubTargetProv{defaultProv: "nonexistent"}
	_, err := pm.getProxyProvider(&model.Config{TargetProvider: "docker"})
	if !errors.Is(err, ErrProxyProviderNotFound) {
		t.Fatalf("expected ErrProxyProviderNotFound, got %v", err)
	}
}

func TestGetProxyProvider_ByGlobalDefault(t *testing.T) {
	t.Parallel()
	cfg := newTestConfig(t)
	cfg.DefaultProxyProvider = "global-ts"

	pm := newTestProxyManager(cfg)
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
	t.Parallel()
	cfg := newTestConfig(t)
	cfg.DefaultProxyProvider = ""

	pm := newTestProxyManager(cfg)
	pm.TargetProviders["docker"] = &stubTargetProv{defaultProv: ""}
	_, err := pm.getProxyProvider(&model.Config{TargetProvider: "docker"})
	if !errors.Is(err, ErrProxyProviderNotFound) {
		t.Fatalf("expected ErrProxyProviderNotFound, got %v", err)
	}
}

func TestGetProxyProvider_TargetProviderNotFound(t *testing.T) {
	t.Parallel()
	pm := newTestProxyManager(newTestConfig(t))
	_, err := pm.getProxyProvider(&model.Config{TargetProvider: "nonexistent"})
	if !errors.Is(err, ErrTargetProviderNotFound) {
		t.Fatalf("expected ErrTargetProviderNotFound, got %v", err)
	}
}

// -- resolveDNSProviderLocked ---------------------------------------------------

func TestResolveDNSProviderLocked_ByProxyConfig(t *testing.T) {
	t.Parallel()
	pm := newTestProxyManager(newTestConfig(t))
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
	pm := newTestProxyManager(newTestConfig(t))
	_, err := pm.resolveDNSProviderLocked(&model.Config{DNSProvider: "missing"})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestResolveDNSProviderLocked_ByGlobalDefault(t *testing.T) {
	t.Parallel()
	cfg := newTestConfig(t)
	cfg.DefaultDNSProvider = "global-dns"

	pm := newTestProxyManager(cfg)
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
	t.Parallel()
	cfg := newTestConfig(t)
	cfg.DefaultDNSProvider = "missing"

	pm := newTestProxyManager(cfg)
	_, err := pm.resolveDNSProviderLocked(&model.Config{})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestResolveDNSProviderLocked_NoneConfigured(t *testing.T) {
	t.Parallel()
	cfg := newTestConfig(t)
	cfg.DefaultDNSProvider = ""

	pm := newTestProxyManager(cfg)
	_, err := pm.resolveDNSProviderLocked(&model.Config{})
	if !errors.Is(err, ErrNoDNSProvider) {
		t.Fatalf("expected ErrNoDNSProvider, got %v", err)
	}
}

func TestResolveDNSProviderLocked_ProxyConfigTakesPrecedence(t *testing.T) {
	t.Parallel()
	cfg := newTestConfig(t)
	cfg.DefaultDNSProvider = "global"

	pm := newTestProxyManager(cfg)
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
	pm := newTestProxyManager(newTestConfig(t))
	pm.TLSProviders["my-tls"] = &stubTLSProv{name: "my-tls"}
	p, err := pm.resolveTLSProviderLocked(&model.Config{TLSProvider: "my-tls"}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Name() != "my-tls" {
		t.Fatalf("got %s", p.Name())
	}
}

func TestResolveTLSProviderLocked_TailscaleName(t *testing.T) {
	t.Parallel()
	pm := newTestProxyManager(newTestConfig(t))
	p, err := pm.resolveTLSProviderLocked(&model.Config{TLSProvider: "tailscale"}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Name() != "tailscale" {
		t.Fatalf("got %s", p.Name())
	}
}

func TestResolveTLSProviderLocked_TailscaleInConfig(t *testing.T) {
	t.Parallel()
	cfg := newTestConfig(t)
	cfg.TLSProviders = map[string]*config.TLSProviderConfig{
		"ts": {Provider: "tailscale"},
	}

	pm := newTestProxyManager(cfg)
	p, err := pm.resolveTLSProviderLocked(&model.Config{TLSProvider: "ts"}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Name() != "tailscale" {
		t.Fatalf("got %s", p.Name())
	}
}

func TestResolveTLSProviderLocked_NotFound(t *testing.T) {
	t.Parallel()
	pm := newTestProxyManager(newTestConfig(t))
	_, err := pm.resolveTLSProviderLocked(&model.Config{TLSProvider: "missing"}, nil)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestResolveTLSProviderLocked_NoneConfigured(t *testing.T) {
	t.Parallel()
	cfg := newTestConfig(t)
	cfg.DefaultTLSProvider = ""

	pm := newTestProxyManager(cfg)
	_, err := pm.resolveTLSProviderLocked(&model.Config{}, nil)
	if !errors.Is(err, ErrNoTLSProvider) {
		t.Fatalf("expected ErrNoTLSProvider, got %v", err)
	}
}

func TestResolveTLSProviderLocked_UsesDefault(t *testing.T) {
	t.Parallel()
	cfg := newTestConfig(t)
	cfg.DefaultTLSProvider = "my-default"

	pm := newTestProxyManager(cfg)
	pm.TLSProviders["my-default"] = &stubTLSProv{name: "my-default"}
	p, err := pm.resolveTLSProviderLocked(&model.Config{}, nil)
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
		wantErr  bool
	}{
		{"https://myapp.tailnet.ts.net:443", "myapp.tailnet.ts.net:443", false},
		{"http://example.com", "example.com", false},
		{"https://host.name/path", "host.name", false},
		{"plain-string", "", true},
		{"", "", true},
		{"://invalid", "", true},
	}
	for _, tc := range tests {
		got, err := extractHost(tc.input)
		if tc.wantErr {
			if err == nil {
				t.Errorf("extractHost(%q) expected error, got %q", tc.input, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("extractHost(%q) unexpected error: %v", tc.input, err)
			continue
		}
		if got != tc.expected {
			t.Errorf("extractHost(%q) = %q, want %q", tc.input, got, tc.expected)
		}
	}
}

// -- addTargetProvider / getTargetProvider -------------------------------------

func TestAddAndGetTargetProvider(t *testing.T) {
	t.Parallel()
	pm := newTestProxyManager(newTestConfig(t))
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
	pm := newTestProxyManager(newTestConfig(t))
	_, ok := pm.getTargetProvider("nonexistent")
	if ok {
		t.Fatal("expected false")
	}
}

// -- targetLocks (additional concurrency test) ---------------------------------

func TestTargetLocks_LocksExclusively(t *testing.T) {
	t.Parallel()
	pm := newTestProxyManager(newTestConfig(t))
	unlock1 := pm.targetLocks.Lock("lock-test")

	locked := make(chan struct{})
	go func() {
		defer pm.targetLocks.Lock("lock-test")()
		close(locked)
	}()

	select {
	case <-locked:
		t.Fatal("second Lock should block")
	case <-time.After(10 * time.Millisecond):
	}

	unlock1()
	<-locked
}

// -- updateProxyCount / cleanupProxyMetrics ------------------------------------

func TestUpdateProxyCount(t *testing.T) {
	pm := newTestProxyManager(newTestConfig(t))
	pm.Proxies["a"] = &Proxy{Config: &model.Config{Hostname: "a"}}
	pm.Proxies["b"] = &Proxy{Config: &model.Config{Hostname: "b"}}
	pm.updateProxyCount()
	if len(pm.Proxies) != 2 {
		t.Fatalf("expected 2 proxies, got %d", len(pm.Proxies))
	}
}

func TestUpdateProxyCount_NilMetrics(t *testing.T) {
	t.Parallel()
	pm := newTestProxyManager(newTestConfig(t))
	pm.metrics = nil
	pm.updateProxyCount()
}

func TestCleanupProxyMetrics(t *testing.T) {
	t.Parallel()
	pm := newTestProxyManager(newTestConfig(t))
	pm.Proxies["test"] = &Proxy{Config: &model.Config{Hostname: "test"}}
	pm.cleanupProxyMetrics("test")
}

func TestCleanupProxyMetrics_NilMetrics(t *testing.T) {
	t.Parallel()
	pm := newTestProxyManager(newTestConfig(t))
	pm.metrics = nil
	pm.cleanupProxyMetrics("test")
}

// -- hostLocks -----------------------------------------------------------------

func TestHostLocks_SameHostname_Serializes(t *testing.T) {
	t.Parallel()
	pm := newTestProxyManager(newTestConfig(t))
	unlock1 := pm.hostLocks.Lock("test-host")

	locked := make(chan struct{})
	go func() {
		defer pm.hostLocks.Lock("test-host")()
		close(locked)
	}()

	select {
	case <-locked:
		t.Fatal("second Lock should block")
	case <-time.After(10 * time.Millisecond):
	}

	unlock1()
	<-locked
}

func TestHostLocks_DifferentHostnames_AreIndependent(t *testing.T) {
	t.Parallel()
	pm := newTestProxyManager(newTestConfig(t))
	unlock1 := pm.hostLocks.Lock("host-1")

	locked := make(chan struct{})
	go func() {
		defer pm.hostLocks.Lock("host-2")()
		close(locked)
	}()

	select {
	case <-locked:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("different hostname Lock should not block")
	}

	unlock1()
}

// -- StopAllProxies concurrency safety -----------------------------------------

func TestStopAllProxies_ConcurrentSafe(t *testing.T) {
	t.Parallel()
	pm := newTestProxyManager(newTestConfig(t))
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			ctx, cancel := context.WithCancel(context.Background())
			name := string(rune('a' + n)) //nolint:gosec
			pm.proxyMu.Lock()
			pm.Proxies[name] = &Proxy{
				Config:        &model.Config{Hostname: name},
				log:           zerolog.Nop(),
				ctx:           ctx,
				cancel:        cancel,
				statusHistory: make([]StatusTransition, 0, maxStatusHistory),
			}
			pm.proxyMu.Unlock()
		}(i)
	}
	wg.Wait()

	pm.StopAllProxies()

	pm.proxyMu.RLock()
	if len(pm.Proxies) != 0 {
		t.Fatalf("expected 0 proxies after StopAllProxies")
	}
	pm.proxyMu.RUnlock()
}

// -- addProxyProvider ----------------------------------------------------------

func TestAddProxyProvider_ConvertsToLowercase(t *testing.T) {
	t.Parallel()
	pm := newTestProxyManager(newTestConfig(t))
	pm.addProxyProvider(&stubProxyProv{}, "MyProv")
	_, ok := pm.ProxyProviders["myprov"]
	if !ok {
		t.Fatal("expected provider stored as lowercase")
	}
}

// -- setupDomainForProxy tests ---------------------------------------------------

type dnsErrorProvider struct{ mockDNSProvider }

func (d *dnsErrorProvider) CreateRecord(_ context.Context, _, _, _ string) error {
	return errors.New("dns create failed")
}

type tlsErrorProvider struct{ mockTLSProvider }

func (t *tlsErrorProvider) Provision(_ context.Context, _ string) error {
	return errors.New("tls provision failed")
}

func newSetupDomainTestProxy(t *testing.T) (*ProxyManager, *Proxy) {
	t.Helper()

	pm := newTestProxyManager(newTestConfig(t))
	pm.dnsLifecycle = dnsproviders.NewLifecycleManager(true)
	pm.tlsLifecycle = tlsproviders.NewTLSLifecycleManager(true)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	p := &Proxy{
		log:           zerolog.Nop(),
		Config:        &model.Config{},
		providerProxy: &stubProviderProxy{url: "https://myapp.tailnet.ts.net"},
		ctx:           ctx,
		cancel:        cancel,
		statusHistory: make([]StatusTransition, 0, maxStatusHistory),
	}

	return pm, p
}

func TestSetupDomainForProxy_HappyPath(t *testing.T) {
	t.Parallel()

	pm, p := newSetupDomainTestProxy(t)
	p.SetDNSAndTLSProviders(&mockDNSProvider{name: "cloudflare"}, &mockTLSProvider{name: "acme"})

	proxyConfig := &model.Config{Domain: "app.example.com"}

	err := pm.setupDomainForProxy(p, proxyConfig)

	assert.NoError(t, err)
	assert.Equal(t, dnsproviders.DNSStatusActive, p.GetDNSStatus())
	assert.Equal(t, tlsproviders.TLSStatusActive, p.GetTLSStatus())
}

func TestSetupDomainForProxy_DNSError(t *testing.T) {
	t.Parallel()

	pm, p := newSetupDomainTestProxy(t)
	p.SetDNSAndTLSProviders(&dnsErrorProvider{}, &mockTLSProvider{name: "acme"})

	proxyConfig := &model.Config{Domain: "app.example.com"}

	err := pm.setupDomainForProxy(p, proxyConfig)

	assert.Error(t, err)
	assert.Equal(t, dnsproviders.DNSStatusError, p.GetDNSStatus())
}

func TestSetupDomainForProxy_TLSError(t *testing.T) {
	t.Parallel()

	pm, p := newSetupDomainTestProxy(t)
	dnsProv := &trackingDNSProvider{name: "cloudflare"}
	p.SetDNSAndTLSProviders(dnsProv, &tlsErrorProvider{})

	proxyConfig := &model.Config{Domain: "app.example.com"}

	err := pm.setupDomainForProxy(p, proxyConfig)

	assert.Error(t, err)
	assert.Equal(t, int64(1), dnsProv.deleteCount.Load(),
		"DNS record must be rolled back when TLS provisioning fails")
	assert.Equal(t, tlsproviders.TLSStatusError, p.GetTLSStatus())
}

func TestSetupDomainForProxy_TailscaleTLSPath(t *testing.T) {
	t.Parallel()

	pm, p := newSetupDomainTestProxy(t)
	p.providerProxy = &stubLocalClientProvider{localClient: nil}
	p.SetDNSAndTLSProviders(&mockDNSProvider{name: "cloudflare"}, &mockTLSProvider{name: "tailscale"})

	proxyConfig := &model.Config{Domain: "app.example.com"}

	err := pm.setupDomainForProxy(p, proxyConfig)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "tailscale local client not available")
}

// -- waitForProxyURL ------------------------------------------------------------

func TestWaitForProxyURL_URLAvailableImmediately(t *testing.T) {
	t.Parallel()
	pm := newTestProxyManager(newTestConfig(t))

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() { cancel() })

	p := &Proxy{
		log:           zerolog.Nop(),
		Config:        &model.Config{},
		providerProxy: &stubProviderProxy{url: "https://myapp.tailnet.ts.net"},
		ctx:           ctx,
		cancel:        cancel,
	}

	host, err := pm.waitForProxyURL(p)
	assert.NoError(t, err)
	assert.Equal(t, "myapp.tailnet.ts.net", host)
}

func TestWaitForProxyURL_URLBecomesAvailableAfterPoll(t *testing.T) {
	t.Parallel()
	pm := newTestProxyManager(newTestConfig(t))

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() { cancel() })

	proxy := &delayedURLProxy{delay: 1 * time.Second}
	p := &Proxy{
		log:           zerolog.Nop(),
		Config:        &model.Config{},
		providerProxy: proxy,
		ctx:           ctx,
		cancel:        cancel,
		urlReady:      make(chan struct{}),
	}

	go func() {
		time.Sleep(1100 * time.Millisecond)
		close(p.urlReady)
	}()

	host, err := pm.waitForProxyURL(p)
	assert.NoError(t, err)
	assert.Equal(t, "delayed.tailnet.ts.net", host)
}

func TestWaitForProxyURL_ContextCancelled(t *testing.T) {
	t.Parallel()
	pm := newTestProxyManager(newTestConfig(t))

	ctx, cancel := context.WithCancel(context.Background())
	p := &Proxy{
		log:           zerolog.Nop(),
		Config:        &model.Config{},
		providerProxy: &stubProviderProxy{url: ""},
		ctx:           ctx,
		cancel:        cancel,
	}

	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	_, err := pm.waitForProxyURL(p)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "context canceled")
}

func TestWaitForProxyURL_Timeout(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() { cancel() })

	p := &Proxy{
		log:           zerolog.Nop(),
		Config:        &model.Config{},
		providerProxy: &stubProviderProxy{url: ""},
		ctx:           ctx,
		cancel:        cancel,
	}

	pm := newTestProxyManager(newTestConfig(t))

	done := make(chan error, 1)
	go func() {
		_, err := pm.waitForProxyURL(p)
		done <- err
	}()

	cancel()

	select {
	case err := <-done:
		assert.Error(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("waitForProxyURL did not return after context cancellation")
	}
}

// delayedURLProxy returns empty URL until the delay has elapsed, then returns a real URL.
type delayedURLProxy struct {
	started time.Time
	stubProviderProxy
	delay time.Duration
	once  sync.Once
}

func (d *delayedURLProxy) GetURL() string {
	d.once.Do(func() { d.started = time.Now() })
	if time.Since(d.started) >= d.delay {
		return "https://delayed.tailnet.ts.net"
	}
	return ""
}

// -- cleanupDomainForProxy ------------------------------------------------------

func TestCleanupDomainForProxy_EmptyDomainSkipsCleanup(t *testing.T) {
	t.Parallel()
	pm := newTestProxyManager(newTestConfig(t))
	pm.dnsLifecycle = dnsproviders.NewLifecycleManager(true)
	pm.tlsLifecycle = tlsproviders.NewTLSLifecycleManager(true)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() { cancel() })

	p := &Proxy{
		log:    zerolog.Nop(),
		Config: &model.Config{Domain: ""},
		ctx:    ctx,
		cancel: cancel,
	}

	pm.cleanupDomainForProxy(p)
	assert.Equal(t, dnsproviders.DNSStatusNone, p.GetDNSStatus())
	assert.Equal(t, tlsproviders.TLSStatusNone, p.GetTLSStatus())
}

func TestCleanupDomainForProxy_NilProvidersResetsStatus(t *testing.T) {
	t.Parallel()
	pm := newTestProxyManager(newTestConfig(t))
	pm.dnsLifecycle = dnsproviders.NewLifecycleManager(true)
	pm.tlsLifecycle = tlsproviders.NewTLSLifecycleManager(true)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() { cancel() })

	p := &Proxy{
		log:    zerolog.Nop(),
		Config: &model.Config{Domain: "app.example.com"},
		ctx:    ctx,
		cancel: cancel,
	}

	p.setDNSStatus(dnsproviders.DNSStatusActive)
	p.setTLSStatus(tlsproviders.TLSStatusActive)

	pm.cleanupDomainForProxy(p)
	assert.Equal(t, dnsproviders.DNSStatusNone, p.GetDNSStatus())
	assert.Equal(t, tlsproviders.TLSStatusNone, p.GetTLSStatus())
}

func TestCleanupDomainForProxy_TLSCleanupErrorLogged(t *testing.T) {
	t.Parallel()
	pm := newTestProxyManager(newTestConfig(t))
	pm.dnsLifecycle = dnsproviders.NewLifecycleManager(true)
	pm.tlsLifecycle = tlsproviders.NewTLSLifecycleManager(true)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() { cancel() })

	p := &Proxy{
		log:    zerolog.Nop(),
		Config: &model.Config{Domain: "app.example.com"},
		ctx:    ctx,
		cancel: cancel,
	}
	p.SetDNSAndTLSProviders(nil, &errorTLSProvider{})

	pm.cleanupDomainForProxy(p)
	assert.Equal(t, dnsproviders.DNSStatusNone, p.GetDNSStatus())
	assert.Equal(t, tlsproviders.TLSStatusNone, p.GetTLSStatus())
}

func TestCleanupDomainForProxy_DNSCleanupErrorLogged(t *testing.T) {
	t.Parallel()
	pm := newTestProxyManager(newTestConfig(t))
	pm.dnsLifecycle = dnsproviders.NewLifecycleManager(true)
	pm.tlsLifecycle = tlsproviders.NewTLSLifecycleManager(true)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() { cancel() })

	p := &Proxy{
		log:    zerolog.Nop(),
		Config: &model.Config{Domain: "app.example.com"},
		ctx:    ctx,
		cancel: cancel,
	}
	p.SetDNSAndTLSProviders(&errorDNSProvider{}, nil)

	pm.cleanupDomainForProxy(p)
	assert.Equal(t, dnsproviders.DNSStatusNone, p.GetDNSStatus())
	assert.Equal(t, tlsproviders.TLSStatusNone, p.GetTLSStatus())
}

func TestCleanupDomainForProxy_HappyPathResetsStatuses(t *testing.T) {
	t.Parallel()
	pm := newTestProxyManager(newTestConfig(t))
	pm.dnsLifecycle = dnsproviders.NewLifecycleManager(true)
	pm.tlsLifecycle = tlsproviders.NewTLSLifecycleManager(true)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() { cancel() })

	p := &Proxy{
		log:    zerolog.Nop(),
		Config: &model.Config{Domain: "app.example.com"},
		ctx:    ctx,
		cancel: cancel,
	}
	p.SetDNSAndTLSProviders(&mockDNSProvider{name: "test-dns"}, &mockTLSProvider{name: "test-tls"})
	p.setDNSStatus(dnsproviders.DNSStatusActive)
	p.setTLSStatus(tlsproviders.TLSStatusActive)

	pm.cleanupDomainForProxy(p)
	assert.Equal(t, dnsproviders.DNSStatusNone, p.GetDNSStatus())
	assert.Equal(t, tlsproviders.TLSStatusNone, p.GetTLSStatus())
}

type errorTLSProvider struct{ mockTLSProvider }

func (e *errorTLSProvider) Cleanup(_ context.Context, _ string) error {
	return errors.New("tls cleanup failed")
}

type errorDNSProvider struct{ mockDNSProvider }

func (e *errorDNSProvider) DeleteRecord(_ context.Context, _, _ string) error {
	return errors.New("dns cleanup failed")
}

// ---------------------------------------------------------------------------
// Bug-demonstrating tests — these SHOULD FAIL with the current code.
// ---------------------------------------------------------------------------

// signalingProxyProv wraps stubProxyProv and closes a channel when
// ResolveAuthKey is called. This lets tests synchronize with newProxy's
// progress — proving the goroutine has passed the early stopping checks.
type signalingProxyProv struct {
	resolveCalled chan struct{}
	stubProxyProv
}

func (s *signalingProxyProv) ResolveAuthKey(cfg *model.Config) (string, error) {
	select {
	case <-s.resolveCalled:
	default:
		close(s.resolveCalled)
	}
	return s.stubProxyProv.ResolveAuthKey(cfg)
}

// TestNewProxy_RegisterProxyFailure_LeaksProviderProxy demonstrates BUG-2:
// when registerProxy fails (e.g. ProxyManager is shutting down), newProxy
// must call p.Close() to release the providerProxy (tsnet.Server) that
// buildProxy created. Without the fix, the proxy is never inserted into
// pm.Proxies and teardownProxy is never called — the tsnet.Server is leaked.
//
// This test calls newProxy end-to-end and deterministically triggers a
// registerProxy failure by exploiting the proxyMu lock as a gate:
//  1. Hold proxyMu so newProxy's goroutine blocks inside closeAndRemoveProxy
//  2. Wait for ResolveAuthKey signal — proves goroutine passed stopping checks
//  3. Set stopping=true while still holding proxyMu
//  4. Release proxyMu — goroutine proceeds to registerProxy which sees
//     stopping=true and returns an error
//  5. Assert providerProxy.Close() was called (the fix)
func TestNewProxy_RegisterProxyFailure_LeaksProviderProxy(t *testing.T) {
	t.Parallel()

	pm := newTestProxyManager(newTestConfig(t))

	providerProxy := &stubProviderProxy{}
	resolveCalled := make(chan struct{})
	pm.ProxyProviders["tailscale"] = &signalingProxyProv{
		stubProxyProv: stubProxyProv{
			newProxyRet: providerProxy,
			resolvedKey: "test-key",
		},
		resolveCalled: resolveCalled,
	}

	proxyConfig := &model.Config{
		Hostname:      "test-leak",
		TargetID:      "test-leak-id",
		ProxyProvider: "tailscale",
		Domain:        "app.example.com",
		Ports: map[string]model.PortConfig{
			"80": {ProxyProtocol: model.ProtoTCP},
		},
	}

	// Hold proxyMu so newProxy blocks at closeAndRemoveProxy's proxyMu.Lock(),
	// giving us a deterministic window to set stopping between the early
	// stopping checks and registerProxy's stopping check.
	pm.proxyMu.Lock()

	errCh := make(chan error, 1)
	go func() {
		_, err := pm.newProxy("test-leak", proxyConfig)
		errCh <- err
	}()

	// Wait until goroutine has called ResolveAuthKey — proves it passed
	// both early stopping checks (newProxy lines 948, 957).
	<-resolveCalled

	// Set stopping while holding proxyMu. The goroutine is blocked at
	// closeAndRemoveProxy's proxyMu.Lock() and will see stopping=true when
	// it reaches registerProxy (mutex provides happens-before guarantee).
	pm.stopping.Store(true)
	pm.proxyMu.Unlock()

	// Goroutine proceeds: closeAndRemoveProxy (no-op), buildProxy (creates
	// providerProxy), registerProxy (stopping=true → error), p.Close() (fix).
	err := <-errCh
	assert.Error(t, err)

	// After the fix, newProxy calls p.Close() on registerProxy failure,
	// which calls providerProxy.Close().
	providerProxy.mtx.Lock()
	closed := providerProxy.closed
	providerProxy.mtx.Unlock()

	assert.True(t, closed,
		"BUG: providerProxy.Close() was never called after registerProxy "+
			"failure — tsnet.Server struct leaked (newProxy missing p.Close() "+
			"on error path)")
}

// TestTeardownProxy_TLSProviderClosedBeforeProxy demonstrates BUG-3:
// teardownProxy calls closeTLSProvider (certmagic cache.Stop()) BEFORE
// p.Close() (which closes listeners). Between these two calls, the TLS
// listener is still accepting connections but the certmagic cache is
// stopped, so in-flight TLS handshakes calling GetCertificate hit a
// stopped cache.
//
// After the fix, the order should be swapped: p.Close() first (close
// listeners), then closeTLSProvider (stop cache).
func TestTeardownProxy_TLSProviderClosedBeforeProxy(t *testing.T) {
	t.Parallel()

	pm := newTestProxyManager(newTestConfig(t))

	var sequence []string
	var seqMu sync.Mutex

	tlsProv := &closeTrackingTLSProv{
		mockTLSProvider: mockTLSProvider{name: "test-tls"},
		log:             &sequence,
		mu:              &seqMu,
	}

	providerProxy := &closeTrackingProviderProxy{
		stubProviderProxy: stubProviderProxy{},
		log:               &sequence,
		mu:                &seqMu,
	}

	ctx, cancel := context.WithCancel(context.Background())
	p := &Proxy{
		log:           zerolog.Nop(),
		Config:        &model.Config{Hostname: "test"},
		ctx:           ctx,
		cancel:        cancel,
		providerProxy: providerProxy,
		statusHistory: make([]StatusTransition, 0, maxStatusHistory),
	}
	p.SetDNSAndTLSProviders(nil, tlsProv)

	pm.teardownProxy(p)

	seqMu.Lock()
	defer seqMu.Unlock()

	tlsPos, proxyPos := -1, -1
	for i, s := range sequence {
		switch s {
		case "tls-close":
			tlsPos = i
		case "proxy-close":
			proxyPos = i
		}
	}

	if tlsPos == -1 {
		t.Fatal("TLS provider Close() was never called during teardown")
	}
	if proxyPos == -1 {
		t.Fatal("providerProxy Close() was never called during teardown")
	}

	if tlsPos < proxyPos {
		t.Errorf("BUG: TLS cache stopped (step %d) before listeners closed (step %d) — "+
			"TLS handshakes can hit stopped certmagic cache. Swap closeTLSProvider and p.Close() "+
			"in teardownProxy", tlsPos, proxyPos)
	}
}

// closeTrackingTLSProv wraps mockTLSProvider and implements tlsproviders.Closer,
// recording the call order for teardownProxy sequence verification.
type closeTrackingTLSProv struct {
	log *[]string
	mu  *sync.Mutex
	mockTLSProvider
}

func (t *closeTrackingTLSProv) Close() error {
	t.mu.Lock()
	*t.log = append(*t.log, "tls-close")
	t.mu.Unlock()
	return nil
}

// closeTrackingProviderProxy wraps stubProviderProxy, recording when Close()
// is called for teardownProxy sequence verification.
type closeTrackingProviderProxy struct {
	log *[]string
	mu  *sync.Mutex
	stubProviderProxy
}

func (c *closeTrackingProviderProxy) Close() error {
	c.mu.Lock()
	*c.log = append(*c.log, "proxy-close")
	c.mu.Unlock()
	return c.stubProviderProxy.Close()
}
