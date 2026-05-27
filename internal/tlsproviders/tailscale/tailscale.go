// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package tailscale

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"strings"

	"tailscale.com/client/local"

	"github.com/almeidapaulopt/tsdproxy/internal/model"
	"github.com/almeidapaulopt/tsdproxy/internal/tlsproviders"
)

// Provider implements tlsproviders.Provider using Tailscale's CertPair.
// Only works for MagicDNS domains (*.ts.net).
type Provider struct {
	lc *local.Client
}

var _ tlsproviders.Provider = (*Provider)(nil)

func New(lc *local.Client) *Provider {
	return &Provider{lc: lc}
}

func (p *Provider) Name() string {
	return model.TLSProviderTailscale
}

func (p *Provider) Provision(ctx context.Context, domain string) error {
	if !isMagicDNSDomain(domain) {
		return fmt.Errorf("tailscale tls: domain %q is not a MagicDNS domain", domain)
	}
	if p.lc == nil {
		return errors.New("tailscale tls: local client is nil")
	}
	_, _, err := p.lc.CertPair(ctx, domain)
	if err != nil {
		return fmt.Errorf("tailscale tls: certpair for %s: %w", domain, err)
	}
	return nil
}

func (p *Provider) GetCertificate(_ context.Context, domain string) (tls.Certificate, error) {
	if p.lc == nil {
		return tls.Certificate{}, errors.New("tailscale tls: local client is nil")
	}
	certPEM, keyPEM, err := p.lc.CertPair(context.Background(), domain)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("tailscale tls: get cert for %s: %w", domain, err)
	}
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("tailscale tls: parse cert for %s: %w", domain, err)
	}
	return cert, nil
}

func (p *Provider) Cleanup(_ context.Context, _ string) error {
	return nil
}

func isMagicDNSDomain(domain string) bool {
	return strings.HasSuffix(strings.ToLower(domain), ".ts.net")
}
