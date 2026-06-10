// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package proxymanager

import (
	"context"
	"crypto/tls"
	"errors"
	"testing"

	"github.com/caddyserver/certmagic"
	"github.com/libdns/libdns"
	"github.com/rs/zerolog"

	"github.com/almeidapaulopt/tsdproxy/internal/config"
	"github.com/almeidapaulopt/tsdproxy/internal/dnsproviders"
	"github.com/almeidapaulopt/tsdproxy/internal/model"
	"github.com/almeidapaulopt/tsdproxy/internal/proxyproviders"
	"github.com/almeidapaulopt/tsdproxy/internal/tlsproviders"
)

// mockDNSProvider is a certmagic-capable DNS provider for testing.
type mockDNSProvider struct {
	name string
}

var (
	_ dnsproviders.Provider = (*mockDNSProvider)(nil)
	_ certmagic.DNSProvider = (*mockDNSProvider)(nil)
)

func (m *mockDNSProvider) Name() string { return m.name }
func (m *mockDNSProvider) CreateRecord(_ context.Context, _, _, _ string) error {
	return nil
}

func (m *mockDNSProvider) DeleteRecord(_ context.Context, _, _ string) error {
	return nil
}

func (m *mockDNSProvider) ValidateRecord(_ context.Context, _, _, _ string) (bool, error) {
	return true, nil
}

func (m *mockDNSProvider) AppendRecords(_ context.Context, _ string, _ []libdns.Record) ([]libdns.Record, error) {
	return nil, nil
}

func (m *mockDNSProvider) DeleteRecords(_ context.Context, _ string, _ []libdns.Record) ([]libdns.Record, error) {
	return nil, nil
}

// mockTLSProvider is a non-ACME TLS provider for testing fallback behavior.
type mockTLSProvider struct {
	name string
}

var _ tlsproviders.Provider = (*mockTLSProvider)(nil)

func (m *mockTLSProvider) Name() string { return m.name }
func (m *mockTLSProvider) Provision(_ context.Context, _ string) error {
	return nil
}

func (m *mockTLSProvider) GetCertificate(_ context.Context, _ string) (tls.Certificate, error) {
	return tls.Certificate{}, nil
}
func (m *mockTLSProvider) Cleanup(_ context.Context, _ string) error { return nil }

func newTestProxyManager() *ProxyManager {
	ctx, cancel := context.WithCancel(context.Background())
	return &ProxyManager{
		ctx:               ctx,
		cancel:            cancel,
		Proxies:           make(ProxyList),
		TargetProviders:   make(TargetProviderList),
		ProxyProviders:    make(ProxyProviderList),
		DNSProviders:      make(DNSProviderList),
		TLSProviders:      make(TLSProviderList),
		statusSubscribers: make(map[*statusSubscription]struct{}),
		log:               zerolog.Nop().With().Str("module", "proxymanager").Logger(),
	}
}

func setupTestConfig(t *testing.T) {
	t.Helper()
	origConfig := config.Config
	t.Cleanup(func() { config.Config = origConfig })
	config.SetTestConfig(t.TempDir(), "")
}

func TestResolveAndSetProviders_PerProxyACMEUsesResolvedDNSProvider(t *testing.T) {
	setupTestConfig(t)

	perProxyDNS := &mockDNSProvider{name: "cloudflare-per-proxy"}
	globalDNS := &mockDNSProvider{name: "cloudflare-global"}

	config.Config.DefaultDNSProvider = "global-dns"
	config.Config.DefaultTLSProvider = "myacme"
	config.Config.TLSProviders = map[string]*config.TLSProviderConfig{
		"myacme": {
			Provider: "acme",
			Email:    "test@example.com",
			CA:       "https://acme-v02.api.letsencrypt.org/directory",
		},
	}

	pm := newTestProxyManager()
	pm.DNSProviders["global-dns"] = globalDNS
	pm.DNSProviders["per-proxy-dns"] = perProxyDNS
	pm.addTLSProviders()

	proxyConfig := &model.Config{
		Hostname:    "testproxy",
		DNSProvider: "per-proxy-dns",
		TLSProvider: "myacme",
	}

	sp := &Proxy{}

	if err := pm.resolveAndSetProviders(sp, proxyConfig); err != nil {
		t.Fatalf("resolveAndSetProviders failed: %v", err)
	}

	sp.mtx.RLock()
	resolvedDNS := sp.dnsProvider
	resolvedTLS := sp.tlsProvider
	sp.mtx.RUnlock()

	if resolvedDNS == nil {
		t.Fatal("expected DNS provider to be set, got nil")
	}
	if resolvedDNS.Name() != "cloudflare-per-proxy" {
		t.Fatalf("expected per-proxy DNS provider %q, got %q", "cloudflare-per-proxy", resolvedDNS.Name())
	}

	if resolvedTLS == nil {
		t.Fatal("expected TLS provider to be set, got nil")
	}
	if resolvedTLS.Name() != "acme" {
		t.Fatalf("expected ACME TLS provider, got %q", resolvedTLS.Name())
	}

	if resolvedDNS.Name() == "cloudflare-global" {
		t.Fatal("BUG: per-proxy ACME is using the global DNS provider instead of the per-proxy one")
	}
}

func TestResolveAndSetProviders_NonACMEDoesNotRecreate(t *testing.T) {
	setupTestConfig(t)

	globalDNS := &mockDNSProvider{name: "global-dns"}
	nonACME := &mockTLSProvider{name: "tailscale"}

	config.Config.DefaultDNSProvider = "global-dns"
	config.Config.DefaultTLSProvider = "tailscale"
	config.Config.TLSProviders = map[string]*config.TLSProviderConfig{}

	pm := newTestProxyManager()
	pm.DNSProviders["global-dns"] = globalDNS
	pm.TLSProviders["tailscale"] = nonACME

	proxyConfig := &model.Config{
		Hostname:    "testproxy",
		DNSProvider: "global-dns",
		TLSProvider: "tailscale",
	}

	sp := &Proxy{}

	if err := pm.resolveAndSetProviders(sp, proxyConfig); err != nil {
		t.Fatalf("resolveAndSetProviders failed: %v", err)
	}

	sp.mtx.RLock()
	resolvedTLS := sp.tlsProvider
	sp.mtx.RUnlock()

	if resolvedTLS == nil {
		t.Fatal("expected TLS provider to be set, got nil")
	}
	if resolvedTLS.Name() != "tailscale" {
		t.Fatalf("expected tailscale TLS provider, got %q", resolvedTLS.Name())
	}
}

func TestNewAndStartProxy_DomainRequiredProviderRejectsNoDomain(t *testing.T) {
	setupTestConfig(t)

	pm := newTestProxyManager()
	pm.ProxyProviders["shared"] = &domainRequiredStub{domainRequired: true}

	proxyConfig := &model.Config{
		Hostname:      "testproxy",
		ProxyProvider: "shared",
		Ports: map[string]model.PortConfig{
			"1": {ProxyProtocol: model.ProtoHTTPS, ProxyPort: 443},
		},
	}

	err := pm.newAndStartProxy("testproxy", proxyConfig)
	if err == nil {
		t.Fatal("expected error when domain-required provider has no domain set, got nil")
	}
	if err.Error() != "proxy provider requires a domain to be set on each proxy" {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestNewAndStartProxy_DomainRequiredProviderAllowsDomain(t *testing.T) {
	setupTestConfig(t)

	pm := newTestProxyManager()
	pm.ProxyProviders["shared"] = &domainRequiredStub{domainRequired: true, failNewProxy: true}

	proxyConfig := &model.Config{
		Hostname:      "testproxy",
		ProxyProvider: "shared",
		Domain:        "app.example.com",
		Ports: map[string]model.PortConfig{
			"1": {ProxyProtocol: model.ProtoHTTPS, ProxyPort: 443},
		},
	}

	err := pm.newAndStartProxy("testproxy", proxyConfig)
	if err == nil {
		t.Fatal("expected error from NewProxy stub, got nil")
	}
	if err.Error() == "proxy provider requires a domain to be set on each proxy" {
		t.Fatal("domain validation should have passed but got domain-required error")
	}
}

// domainRequiredStub implements proxyproviders.Provider and
// proxyproviders.DomainRequiredProvider for testing.
type domainRequiredStub struct {
	domainRequired bool
	failNewProxy   bool
}

func (s *domainRequiredStub) IsDomainRequired() bool { return s.domainRequired }
func (s *domainRequiredStub) ResolveAuthKey(_ *model.Config) (string, error) {
	return "", nil
}

func (s *domainRequiredStub) NewProxy(_ *model.Config) (proxyproviders.ProxyInterface, error) {
	if s.failNewProxy {
		return nil, errors.New("stub: NewProxy intentionally failed")
	}
	return nil, nil //nolint:nilnil
}

func TestResolveAndSetProviders_ACMEWithNonCertmagicDNSFails(t *testing.T) {
	setupTestConfig(t)

	nonCertmagicDNS := &struct {
		dnsproviders.Provider
	}{
		Provider: &mockDNSProvider{name: "magicdns-no-certmagic"},
	}

	config.Config.DefaultDNSProvider = "bad-dns"
	config.Config.DefaultTLSProvider = "myacme"
	config.Config.TLSProviders = map[string]*config.TLSProviderConfig{
		"myacme": {
			Provider: "acme",
			Email:    "test@example.com",
		},
	}

	pm := newTestProxyManager()
	pm.DNSProviders["bad-dns"] = nonCertmagicDNS
	pm.addTLSProviders()

	proxyConfig := &model.Config{
		Hostname:    "testproxy",
		DNSProvider: "bad-dns",
		TLSProvider: "myacme",
	}

	sp := &Proxy{}

	if err := pm.resolveAndSetProviders(sp, proxyConfig); err == nil {
		t.Fatal("expected error when DNS provider does not implement certmagic.DNSProvider, got nil")
	}
}
