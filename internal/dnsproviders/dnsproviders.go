// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package dnsproviders

import (
	"context"

	"github.com/almeidapaulopt/tsdproxy/internal/lifecycle"
)

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

	DNSStatus = lifecycle.Status
)

const (
	DNSStatusNone    DNSStatus = lifecycle.StatusNone
	DNSStatusPending DNSStatus = lifecycle.StatusPending
	DNSStatusActive  DNSStatus = lifecycle.StatusActive
	DNSStatusError   DNSStatus = lifecycle.StatusError
)
