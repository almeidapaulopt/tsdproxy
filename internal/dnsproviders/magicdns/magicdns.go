// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package magicdns

import (
	"context"

	"github.com/almeidapaulopt/tsdproxy/internal/dnsproviders"
)

// Provider is a no-op DNS provider for MagicDNS (Tailscale internal DNS).
// Tailscale handles DNS internally, so no external DNS operations are needed.
type Provider struct{}

var _ dnsproviders.Provider = (*Provider)(nil)

// New creates a new MagicDNS provider.
func New() *Provider {
	return &Provider{}
}

// Name returns the provider name.
func (p *Provider) Name() string {
	return "magicdns"
}

// CreateRecord is a no-op for MagicDNS.
func (p *Provider) CreateRecord(_ context.Context, _, _, _ string) error {
	return nil
}

// DeleteRecord is a no-op for MagicDNS.
func (p *Provider) DeleteRecord(_ context.Context, _, _ string) error {
	return nil
}

// ValidateRecord always returns true for MagicDNS — Tailscale handles DNS internally.
func (p *Provider) ValidateRecord(_ context.Context, _, _, _ string) (bool, error) {
	return true, nil
}
