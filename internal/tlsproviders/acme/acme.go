// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package acme

import (
	"context"
	"crypto/tls"
	"fmt"

	"github.com/caddyserver/certmagic"

	"github.com/almeidapaulopt/tsdproxy/internal/model"
	"github.com/almeidapaulopt/tsdproxy/internal/tlsproviders"
)

// Provider implements tlsproviders.Provider using certmagic ACME DNS-01.
type Provider struct {
	cfg *certmagic.Config
}

var _ tlsproviders.Provider = (*Provider)(nil)

type Config struct {
	Email       string
	CA          string
	DNSProvider certmagic.DNSProvider
	CertStorage string
}

func New(acmeCfg Config) (*Provider, error) {
	if acmeCfg.CA == "" {
		acmeCfg.CA = certmagic.LetsEncryptProductionCA
	}

	cfg := certmagic.NewDefault()

	issuer := certmagic.NewACMEIssuer(cfg, certmagic.ACMEIssuer{
		CA:    acmeCfg.CA,
		Email: acmeCfg.Email,
		DNS01Solver: &certmagic.DNS01Solver{
			DNSManager: certmagic.DNSManager{
				DNSProvider: acmeCfg.DNSProvider,
			},
		},
	})

	cfg.Issuers = []certmagic.Issuer{issuer}

	if acmeCfg.CertStorage != "" {
		cfg.Storage = &certmagic.FileStorage{Path: acmeCfg.CertStorage}
	}

	return &Provider{cfg: cfg}, nil
}

func (p *Provider) Name() string {
	return model.TLSProviderACME
}

func (p *Provider) Provision(ctx context.Context, domain string) error {
	if err := p.cfg.ManageSync(ctx, []string{domain}); err != nil {
		return fmt.Errorf("acme: provision cert for %s: %w", domain, err)
	}
	return nil
}

func (p *Provider) GetCertificate(ctx context.Context, domain string) (tls.Certificate, error) {
	cert, err := p.cfg.CacheManagedCertificate(ctx, domain)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("acme: get cert for %s: %w", domain, err)
	}
	return cert.Certificate, nil
}

func (p *Provider) Cleanup(_ context.Context, _ string) error {
	// certmagic handles renewal and storage automatically
	// explicit cleanup is not needed for ACME certificates
	return nil
}
