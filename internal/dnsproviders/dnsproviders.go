// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package dnsproviders

import "context"

type (
	// Provider interface for DNS providers.
	Provider interface {
		// Name returns the provider name (e.g. "magicdns", "cloudflare").
		Name() string
		// CreateRecord creates a DNS record of the given type with the given value.
		CreateRecord(ctx context.Context, domain, recordType, value string) error
		// DeleteRecord deletes a DNS record of the given type for the domain.
		DeleteRecord(ctx context.Context, domain, recordType string) error
		// ValidateRecord checks if a DNS record exists with the expected value.
		ValidateRecord(ctx context.Context, domain, recordType, expectedValue string) (bool, error)
	}

	// DNSStatus represents the current state of DNS configuration for a proxy.
	DNSStatus int
)

const (
	DNSStatusNone DNSStatus = iota
	DNSStatusPending
	DNSStatusActive
	DNSStatusError
)

// String returns a human-readable representation of the DNSStatus.
func (s DNSStatus) String() string {
	switch s {
	case DNSStatusNone:
		return "none"
	case DNSStatusPending:
		return "pending"
	case DNSStatusActive:
		return "active"
	case DNSStatusError:
		return "error"
	default:
		return "unknown"
	}
}
