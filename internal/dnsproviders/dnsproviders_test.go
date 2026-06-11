// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package dnsproviders

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDNSStatusValues(t *testing.T) {
	assert.Equal(t, DNSStatus(0), DNSStatusNone)
	assert.Equal(t, DNSStatus(1), DNSStatusPending)
	assert.Equal(t, DNSStatus(2), DNSStatusActive)
	assert.Equal(t, DNSStatus(3), DNSStatusError)
}

func TestDNSStatusString(t *testing.T) {
	tests := []struct {
		expected string
		status   DNSStatus
	}{
		{"none", DNSStatusNone},
		{"pending", DNSStatusPending},
		{"active", DNSStatusActive},
		{"error", DNSStatusError},
		{"unknown", DNSStatus(99)},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.expected, tt.status.String())
	}
}

func TestDNSStatusString_AllConstants(t *testing.T) {
	assert.Equal(t, "none", DNSStatusNone.String())
	assert.Equal(t, "pending", DNSStatusPending.String())
	assert.Equal(t, "active", DNSStatusActive.String())
	assert.Equal(t, "error", DNSStatusError.String())
}

func TestDNSStatusZeroValue(t *testing.T) {
	var s DNSStatus
	assert.Equal(t, DNSStatusNone, s, "zero value of DNSStatus should be DNSStatusNone")
	assert.Equal(t, "none", s.String())
}

type testMockProvider struct {
	name string
}

func (m *testMockProvider) Name() string { return m.name }
func (m *testMockProvider) CreateRecord(_ context.Context, _, _, _ string) error {
	return nil
}

func (m *testMockProvider) DeleteRecord(_ context.Context, _, _ string) error {
	return nil
}

func (m *testMockProvider) ValidateRecord(_ context.Context, _, _, _ string) (bool, error) {
	return true, nil
}

func TestProviderInterface_Compliance(_ *testing.T) {
	var _ Provider = (*testMockProvider)(nil)
}

func TestProviderInterface_Methods(t *testing.T) {
	p := &testMockProvider{name: "test-dns"}

	assert.Equal(t, "test-dns", p.Name())

	err := p.CreateRecord(context.Background(), "example.com", "A", "1.2.3.4")
	assert.NoError(t, err)

	err = p.DeleteRecord(context.Background(), "example.com", "A")
	assert.NoError(t, err)

	ok, err := p.ValidateRecord(context.Background(), "example.com", "A", "1.2.3.4")
	assert.NoError(t, err)
	assert.True(t, ok)
}

type testErrorProvider struct {
	createErr   error
	deleteErr   error
	validateErr error
}

func (e *testErrorProvider) Name() string { return "error-dns" }
func (e *testErrorProvider) CreateRecord(_ context.Context, _, _, _ string) error {
	return e.createErr
}

func (e *testErrorProvider) DeleteRecord(_ context.Context, _, _ string) error {
	return e.deleteErr
}

func (e *testErrorProvider) ValidateRecord(_ context.Context, _, _, _ string) (bool, error) {
	return false, e.validateErr
}

func TestProviderInterface_ErrorPaths(t *testing.T) {
	p := &testErrorProvider{
		createErr:   assert.AnError,
		deleteErr:   assert.AnError,
		validateErr: assert.AnError,
	}

	assert.Equal(t, "error-dns", p.Name())

	err := p.CreateRecord(context.Background(), "example.com", "A", "1.2.3.4")
	assert.Error(t, err)

	err = p.DeleteRecord(context.Background(), "example.com", "A")
	assert.Error(t, err)

	ok, err := p.ValidateRecord(context.Background(), "example.com", "A", "1.2.3.4")
	assert.Error(t, err)
	assert.False(t, ok)
}

func TestProviderInterface_ContextCancellation(t *testing.T) {
	p := &testMockProvider{name: "cancel-test"}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := p.CreateRecord(ctx, "example.com", "A", "1.2.3.4")
	assert.NoError(t, err, "mock provider ignores context cancellation")
}

func TestDNSStatus_UnknownOutOfRange(t *testing.T) {
	assert.Equal(t, "unknown", DNSStatus(-1).String())
	assert.Equal(t, "unknown", DNSStatus(100).String())
}
