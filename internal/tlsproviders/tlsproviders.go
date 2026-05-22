// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package tlsproviders

import (
	"context"
	"crypto/tls"
)

type (
	// Provider interface for TLS certificate providers.
	Provider interface {
		// Name returns the provider name (e.g. "tailscale", "acme").
		Name() string
		// Provision provisions a TLS certificate for the given domain.
		Provision(ctx context.Context, domain string) error
		// GetCertificate returns the TLS certificate for the given domain.
		GetCertificate(ctx context.Context, domain string) (tls.Certificate, error)
		// Cleanup removes certificate resources for the given domain.
		Cleanup(ctx context.Context, domain string) error
	}

	// TLSStatus represents the current state of TLS provisioning for a proxy.
	TLSStatus int
)

const (
	TLSStatusNone TLSStatus = iota
	TLSStatusPending
	TLSStatusActive
	TLSStatusError
)

// String returns a human-readable representation of the TLSStatus.
func (s TLSStatus) String() string {
	switch s {
	case TLSStatusNone:
		return "none"
	case TLSStatusPending:
		return "pending"
	case TLSStatusActive:
		return "active"
	case TLSStatusError:
		return "error"
	default:
		return "unknown"
	}
}
