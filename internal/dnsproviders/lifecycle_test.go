// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package dnsproviders

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockProvider struct {
	createErr   error
	validateErr error
	deleteErr   error
	validateOk  bool
	created     bool
	deleted     bool
}

func (m *mockProvider) Name() string { return "mock" }

func (m *mockProvider) CreateRecord(_ context.Context, _, _, _ string) error {
	m.created = true
	return m.createErr
}

func (m *mockProvider) DeleteRecord(_ context.Context, _, _ string) error {
	m.deleted = true
	return m.deleteErr
}

func (m *mockProvider) ValidateRecord(_ context.Context, _, _, _ string) (bool, error) {
	return m.validateOk, m.validateErr
}

func TestLifecycle_SetupDNS_Success(t *testing.T) {
	p := &mockProvider{validateOk: true}
	lm := NewLifecycleManager(true)

	err := lm.SetupDNS(context.Background(), p, "app.example.com", "myapp.ts.net")
	require.NoError(t, err)
	assert.True(t, p.created)
	assert.Equal(t, DNSStatusActive, lm.GetStatus("app.example.com"))
}

func TestLifecycle_SetupDNS_CreateFails(t *testing.T) {
	p := &mockProvider{createErr: errors.New("api error")}
	lm := NewLifecycleManager(true)

	err := lm.SetupDNS(context.Background(), p, "app.example.com", "myapp.ts.net")
	require.Error(t, err)
	assert.Equal(t, DNSStatusError, lm.GetStatus("app.example.com"))
}

func TestLifecycle_SetupDNS_ValidationFails(t *testing.T) {
	p := &mockProvider{validateOk: false}
	lm := NewLifecycleManager(true)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	err := lm.SetupDNS(ctx, p, "app.example.com", "myapp.ts.net")
	require.Error(t, err)
	assert.Equal(t, DNSStatusError, lm.GetStatus("app.example.com"))
}

func TestLifecycle_CleanupDNS(t *testing.T) {
	p := &mockProvider{validateOk: true}
	lm := NewLifecycleManager(true)

	require.NoError(t, lm.SetupDNS(context.Background(), p, "app.example.com", "myapp.ts.net"))
	require.NoError(t, lm.CleanupDNS(context.Background(), p, "app.example.com"))
	assert.True(t, p.deleted)
}

func TestLifecycle_CleanupDNS_Skipped(t *testing.T) {
	p := &mockProvider{validateOk: true}
	lm := NewLifecycleManager(false)

	require.NoError(t, lm.SetupDNS(context.Background(), p, "app.example.com", "myapp.ts.net"))
	require.NoError(t, lm.CleanupDNS(context.Background(), p, "app.example.com"))
	assert.False(t, p.deleted)
}

func TestLifecycle_GetStatus_Unknown(t *testing.T) {
	lm := NewLifecycleManager(true)
	assert.Equal(t, DNSStatusNone, lm.GetStatus("unknown.example.com"))
}
