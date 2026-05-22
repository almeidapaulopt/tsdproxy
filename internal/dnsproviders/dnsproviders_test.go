// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package dnsproviders

import (
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
