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
