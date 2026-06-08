// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package tlsproviders

import (
	"context"
	"crypto/tls"

	"github.com/almeidapaulopt/tsdproxy/internal/lifecycle"
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

	TLSStatus = lifecycle.Status
)

const (
	TLSStatusNone    TLSStatus = lifecycle.StatusNone
	TLSStatusPending TLSStatus = lifecycle.StatusPending
	TLSStatusActive  TLSStatus = lifecycle.StatusActive
	TLSStatusError   TLSStatus = lifecycle.StatusError
)
