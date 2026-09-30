// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package proxymanager

import (
	"context"
	"crypto/tls"
	"testing"

	"github.com/caddyserver/certmagic"
	"github.com/libdns/libdns"
	"github.com/rs/zerolog"

	"github.com/almeidapaulopt/tsdproxy/internal/config"
	"github.com/almeidapaulopt/tsdproxy/internal/dnsproviders"
	"github.com/almeidapaulopt/tsdproxy/internal/tlsproviders"
)

// -- Shared mock DNS provider -------------------------------------------------

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

// -- Shared mock TLS provider -------------------------------------------------

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

// -- Shared ProxyManager / config test factories -----------------------------

func newTestProxyManager(cfg *config.Data) *ProxyManager {
	ctx, cancel := context.WithCancel(context.Background())
	return &ProxyManager{
		ctx:               ctx,
		cancel:            cancel,
		cfg:               cfg,
		Proxies:           make(ProxyList),
		targetIndex:       make(map[string]string),
		TargetProviders:   make(TargetProviderList),
		ProxyProviders:    make(ProxyProviderList),
		DNSProviders:      make(DNSProviderList),
		TLSProviders:      make(TLSProviderList),
		statusSubscribers: make(map[*statusSubscription]struct{}),
		log:               zerolog.Nop().With().Str("module", "proxymanager").Logger(),
		targetLocks:       newKeyedLocks(),
		hostLocks:         newKeyedLocks(),
	}
}

func newTestConfig(t *testing.T) *config.Data {
	t.Helper()
	return config.NewTestData(t.TempDir(), "")
}
