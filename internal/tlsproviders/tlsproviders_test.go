// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package tlsproviders

import (
	"context"
	"crypto/tls"
	"testing"

	"github.com/stretchr/testify/assert"
)

// mockProvider verifies the Provider interface compile-time check.
type mockProvider struct{}

var _ Provider = (*mockProvider)(nil)

func (m *mockProvider) Name() string                                { return "mock" }
func (m *mockProvider) Provision(_ context.Context, _ string) error { return nil }
func (m *mockProvider) GetCertificate(_ context.Context, _ string) (tls.Certificate, error) {
	return tls.Certificate{}, nil
}
func (m *mockProvider) Cleanup(_ context.Context, _ string) error { return nil }

func TestTLSStatusValues(t *testing.T) {
	assert.Equal(t, TLSStatus(0), TLSStatusNone)
	assert.Equal(t, TLSStatus(1), TLSStatusPending)
	assert.Equal(t, TLSStatus(2), TLSStatusActive)
	assert.Equal(t, TLSStatus(3), TLSStatusError)
}

func TestTLSStatusString(t *testing.T) {
	tests := []struct {
		expected string
		status   TLSStatus
	}{
		{"none", TLSStatusNone},
		{"pending", TLSStatusPending},
		{"active", TLSStatusActive},
		{"error", TLSStatusError},
		{"unknown", TLSStatus(99)},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.expected, tt.status.String())
	}
}

func TestMockProviderImplementsInterface(t *testing.T) {
	var p Provider = &mockProvider{}
	assert.Equal(t, "mock", p.Name())
	assert.NoError(t, p.Provision(context.Background(), "example.com"))
	assert.NoError(t, p.Cleanup(context.Background(), "example.com"))
}
