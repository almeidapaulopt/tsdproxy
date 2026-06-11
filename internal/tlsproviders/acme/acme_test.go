// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package acme

import (
	"context"
	"testing"

	"github.com/libdns/libdns"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockDNSProvider struct{}

func (m *mockDNSProvider) AppendRecords(_ context.Context, _ string, _ []libdns.Record) ([]libdns.Record, error) {
	return nil, nil
}

func (m *mockDNSProvider) DeleteRecords(_ context.Context, _ string, _ []libdns.Record) ([]libdns.Record, error) {
	return nil, nil
}

func TestNew(t *testing.T) {
	p, err := New(Config{
		Email:       "test@example.com",
		DNSProvider: &mockDNSProvider{},
	})
	require.NoError(t, err)
	require.NotNil(t, p)
}

func TestProvider_Name(t *testing.T) {
	p, err := New(Config{
		DNSProvider: &mockDNSProvider{},
	})
	require.NoError(t, err)
	assert.Equal(t, "acme", p.Name())
}

func TestProvider_Cleanup(t *testing.T) {
	p, err := New(Config{
		DNSProvider: &mockDNSProvider{},
	})
	require.NoError(t, err)
	err = p.Cleanup(context.Background(), "app.example.com")
	assert.NoError(t, err)
}

func TestNew_DefaultCA(t *testing.T) {
	p, err := New(Config{
		DNSProvider: &mockDNSProvider{},
	})
	require.NoError(t, err)
	require.NotNil(t, p)
}

func TestNew_WithCertStorage(t *testing.T) {
	p, err := New(Config{
		Email:       "test@example.com",
		DNSProvider: &mockDNSProvider{},
		DataDir:     t.TempDir(),
	})
	require.NoError(t, err)
	require.NotNil(t, p)
}

func TestNew_ExplicitCertStorageOverridesDataDir(t *testing.T) {
	tmpDir := t.TempDir()
	p, err := New(Config{
		Email:       "test@example.com",
		DNSProvider: &mockDNSProvider{},
		DataDir:     t.TempDir(),
		CertStorage: tmpDir,
	})
	require.NoError(t, err)
	require.NotNil(t, p)
}

func TestNew_ExplicitCA(t *testing.T) {
	p, err := New(Config{
		Email:       "test@example.com",
		CA:          "https://acme-staging-v02.api.letsencrypt.org/directory",
		DNSProvider: &mockDNSProvider{},
	})
	require.NoError(t, err)
	require.NotNil(t, p)
}

func TestProvider_NameReturnsACME(t *testing.T) {
	p, err := New(Config{
		DNSProvider: &mockDNSProvider{},
	})
	require.NoError(t, err)
	assert.Equal(t, "acme", p.Name())
}

func TestProvider_Provision_ContextCanceled(t *testing.T) {
	p, err := New(Config{
		Email:       "test@example.com",
		CA:          "https://acme-staging-v02.api.letsencrypt.org/directory",
		DNSProvider: &mockDNSProvider{},
		CertStorage: t.TempDir(),
	})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err = p.Provision(ctx, "test.example.com")
	assert.Error(t, err, "expected error when context is canceled")
}

func TestProvider_GetCertificate_NoCert(t *testing.T) {
	p, err := New(Config{
		Email:       "test@example.com",
		CA:          "https://acme-staging-v02.api.letsencrypt.org/directory",
		DNSProvider: &mockDNSProvider{},
		CertStorage: t.TempDir(),
	})
	require.NoError(t, err)

	_, err = p.GetCertificate(context.Background(), "nonexistent.example.com")
	assert.Error(t, err, "expected error when no cert exists for domain")
}

func TestProvider_Cleanup_NoError(t *testing.T) {
	p, err := New(Config{
		DNSProvider: &mockDNSProvider{},
	})
	require.NoError(t, err)
	err = p.Cleanup(context.Background(), "app.example.com")
	assert.NoError(t, err)
}

func TestNew_NilDNSProvider(t *testing.T) {
	p, err := New(Config{
		Email:       "test@example.com",
		DNSProvider: nil,
	})
	require.NoError(t, err)
	require.NotNil(t, p)
}

func TestProvider_Provision_EmptyDomain(t *testing.T) {
	p, err := New(Config{
		Email:       "test@example.com",
		CA:          "https://acme-staging-v02.api.letsencrypt.org/directory",
		DNSProvider: &mockDNSProvider{},
		CertStorage: t.TempDir(),
	})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err = p.Provision(ctx, "")
	assert.Error(t, err, "expected error for empty domain with canceled context")
}
