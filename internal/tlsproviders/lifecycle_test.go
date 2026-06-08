// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package tlsproviders

import (
	"context"
	"crypto/tls"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockTLSProvider struct {
	provisionErr error
	cert         *tls.Certificate
	certErr      error
	cleanupErr   error
	provisioned  bool
	cleaned      bool
}

func (m *mockTLSProvider) Name() string { return "mock" }

func (m *mockTLSProvider) Provision(_ context.Context, _ string) error {
	m.provisioned = true
	return m.provisionErr
}

func (m *mockTLSProvider) GetCertificate(_ context.Context, _ string) (tls.Certificate, error) {
	if m.cert != nil {
		return *m.cert, nil
	}
	return tls.Certificate{}, m.certErr
}

func (m *mockTLSProvider) Cleanup(_ context.Context, _ string) error {
	m.cleaned = true
	return m.cleanupErr
}

func TestTLSLifecycle_Provision_Success(t *testing.T) {
	p := &mockTLSProvider{}
	lm := NewTLSLifecycleManager(true)

	err := lm.Provision(context.Background(), p, "app.example.com")
	require.NoError(t, err)
	assert.True(t, p.provisioned)
	assert.Equal(t, TLSStatusActive, lm.GetStatus("app.example.com"))
}

func TestTLSLifecycle_Provision_Fails(t *testing.T) {
	p := &mockTLSProvider{provisionErr: errors.New("cert error")}
	lm := NewTLSLifecycleManager(true)

	err := lm.Provision(context.Background(), p, "app.example.com")
	require.Error(t, err)
	assert.Equal(t, TLSStatusError, lm.GetStatus("app.example.com"))
}

func TestTLSLifecycle_Cleanup(t *testing.T) {
	p := &mockTLSProvider{}
	lm := NewTLSLifecycleManager(true)

	require.NoError(t, lm.Provision(context.Background(), p, "app.example.com"))
	require.NoError(t, lm.Cleanup(context.Background(), p, "app.example.com"))
	assert.True(t, p.cleaned)
}

func TestTLSLifecycle_Cleanup_Skipped(t *testing.T) {
	p := &mockTLSProvider{}
	lm := NewTLSLifecycleManager(false)

	require.NoError(t, lm.Provision(context.Background(), p, "app.example.com"))
	require.NoError(t, lm.Cleanup(context.Background(), p, "app.example.com"))
	assert.False(t, p.cleaned)
}

func TestTLSLifecycle_GetStatus_Unknown(t *testing.T) {
	lm := NewTLSLifecycleManager(true)
	assert.Equal(t, TLSStatusNone, lm.GetStatus("unknown.example.com"))
}
