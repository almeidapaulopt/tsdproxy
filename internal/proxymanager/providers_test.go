// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package proxymanager

import (
	"errors"
	"testing"

	"github.com/almeidapaulopt/tsdproxy/internal/config"
	"github.com/almeidapaulopt/tsdproxy/internal/dnsproviders"
	"github.com/almeidapaulopt/tsdproxy/internal/model"
	"github.com/almeidapaulopt/tsdproxy/internal/proxyproviders"
)

func TestResolveAndSetProviders_PerProxyACMEUsesResolvedDNSProvider(t *testing.T) {
	cfg := newTestConfig(t)
	cfg.DefaultDNSProvider = "global-dns"
	cfg.DefaultTLSProvider = "myacme"
	cfg.TLSProviders = map[string]*config.TLSProviderConfig{
		"myacme": {
			Provider: "acme",
			Email:    "test@example.com",
			CA:       "https://acme-v02.api.letsencrypt.org/directory",
		},
	}

	perProxyDNS := &mockDNSProvider{name: "cloudflare-per-proxy"}
	globalDNS := &mockDNSProvider{name: "cloudflare-global"}

	pm := newTestProxyManager(cfg)
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
	cfg := newTestConfig(t)
	cfg.DefaultDNSProvider = "global-dns"
	cfg.DefaultTLSProvider = "tailscale"
	cfg.TLSProviders = map[string]*config.TLSProviderConfig{}

	globalDNS := &mockDNSProvider{name: "global-dns"}
	nonACME := &mockTLSProvider{name: "tailscale"}

	pm := newTestProxyManager(cfg)
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

func TestRestartProxyLocked_DomainRequiredProviderRejectsNoDomain(t *testing.T) {
	cfg := newTestConfig(t)

	pm := newTestProxyManager(cfg)
	pm.ProxyProviders["shared"] = &domainRequiredStub{domainRequired: true}

	proxyConfig := &model.Config{
		Hostname:      "testproxy",
		ProxyProvider: "shared",
		Ports: map[string]model.PortConfig{
			"1": {ProxyProtocol: model.ProtoHTTPS, ProxyPort: 443},
		},
	}

	err := pm.restartProxyLocked("testproxy", proxyConfig)
	if err == nil {
		t.Fatal("expected error when domain-required provider has no domain set, got nil")
	}
	if err.Error() != "proxy provider requires a domain to be set on each proxy" {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestRestartProxyLocked_DomainRequiredProviderAllowsDomain(t *testing.T) {
	cfg := newTestConfig(t)

	pm := newTestProxyManager(cfg)
	pm.ProxyProviders["shared"] = &domainRequiredStub{domainRequired: true, failNewProxy: true}

	proxyConfig := &model.Config{
		Hostname:      "testproxy",
		ProxyProvider: "shared",
		Domain:        "app.example.com",
		Ports: map[string]model.PortConfig{
			"1": {ProxyProtocol: model.ProtoHTTPS, ProxyPort: 443},
		},
	}

	err := pm.restartProxyLocked("testproxy", proxyConfig)
	if err == nil {
		t.Fatal("expected error from NewProxy stub, got nil")
	}
	if err.Error() == "proxy provider requires a domain to be set on each proxy" {
		t.Fatal("domain validation should have passed but got domain-required error")
	}
}

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
	cfg := newTestConfig(t)
	cfg.DefaultDNSProvider = "bad-dns"
	cfg.DefaultTLSProvider = "myacme"
	cfg.TLSProviders = map[string]*config.TLSProviderConfig{
		"myacme": {
			Provider: "acme",
			Email:    "test@example.com",
		},
	}

	nonCertmagicDNS := &struct {
		dnsproviders.Provider
	}{
		Provider: &mockDNSProvider{name: "magicdns-no-certmagic"},
	}

	pm := newTestProxyManager(cfg)
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

func TestResolveDNSProviderForACME_DefaultFound(t *testing.T) {
	cfg := newTestConfig(t)
	cfg.DefaultDNSProvider = "cloudflare"

	pm := newTestProxyManager(cfg)
	pm.DNSProviders["cloudflare"] = &mockDNSProvider{name: "cloudflare"}

	provider, err := pm.resolveDNSProviderForACME()
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if provider == nil {
		t.Fatal("expected non-nil provider")
	}
}

func TestResolveDNSProviderForACME_DefaultNotFound(t *testing.T) {
	cfg := newTestConfig(t)
	cfg.DefaultDNSProvider = "nonexistent"

	pm := newTestProxyManager(cfg)

	_, err := pm.resolveDNSProviderForACME()
	if err == nil {
		t.Fatal("expected error for missing default DNS provider")
	}
	if err.Error() != `default dns provider "nonexistent" not found` {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestResolveDNSProviderForACME_DefaultNotCertmagic(t *testing.T) {
	cfg := newTestConfig(t)
	cfg.DefaultDNSProvider = "magicdns"

	nonCertmagic := &struct{ dnsproviders.Provider }{
		Provider: &mockDNSProvider{name: "magicdns"},
	}

	pm := newTestProxyManager(cfg)
	pm.DNSProviders["magicdns"] = nonCertmagic

	_, err := pm.resolveDNSProviderForACME()
	if err == nil {
		t.Fatal("expected error for non-certmagic DNS provider")
	}
	if err.Error() != `dns provider "magicdns" does not support ACME DNS-01 challenges` {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestResolveDNSProviderForACME_NoDefaultFindsFirst(t *testing.T) {
	cfg := newTestConfig(t)
	cfg.DefaultDNSProvider = ""

	nonCertmagic := &struct{ dnsproviders.Provider }{
		Provider: &mockDNSProvider{name: "magicdns"},
	}

	pm := newTestProxyManager(cfg)
	pm.DNSProviders["magicdns"] = nonCertmagic
	pm.DNSProviders["cloudflare"] = &mockDNSProvider{name: "cloudflare"}

	provider, err := pm.resolveDNSProviderForACME()
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if provider == nil {
		t.Fatal("expected non-nil provider")
	}
}

func TestResolveDNSProviderForACME_NoCapableProvider(t *testing.T) {
	cfg := newTestConfig(t)
	cfg.DefaultDNSProvider = ""

	nonCertmagic := &struct{ dnsproviders.Provider }{
		Provider: &mockDNSProvider{name: "magicdns"},
	}

	pm := newTestProxyManager(cfg)
	pm.DNSProviders["magicdns"] = nonCertmagic

	_, err := pm.resolveDNSProviderForACME()
	if err == nil {
		t.Fatal("expected error when no certmagic-capable DNS provider exists")
	}
	if err.Error() != "no DNS provider capable of ACME DNS-01 (need a provider like Cloudflare)" {
		t.Fatalf("unexpected error message: %v", err)
	}
}
