// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package acme

import (
	"context"
	"crypto/tls"
	"fmt"
	"path/filepath"

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
	DataDir     string // base data directory for default cert storage
}

func New(acmeCfg Config) (*Provider, error) {
	if acmeCfg.CA == "" {
		acmeCfg.CA = certmagic.LetsEncryptProductionCA
	}

	// Default CertStorage to {DataDir}/certmagic when not explicitly set.
	if acmeCfg.CertStorage == "" && acmeCfg.DataDir != "" {
		acmeCfg.CertStorage = filepath.Join(acmeCfg.DataDir, "certmagic")
	}

	var cfg *certmagic.Config

	// Create a private cache for this provider instance instead of using
	// certmagic.NewDefault()'s global cache. This prevents config cross-talk
	// when multiple ACME instances (global + per-proxy) coexist.
	cache := certmagic.NewCache(certmagic.CacheOptions{
		GetConfigForCert: func(_ certmagic.Certificate) (*certmagic.Config, error) {
			return cfg, nil
		},
	})

	cfg = certmagic.New(cache, certmagic.Config{
		Storage: &certmagic.FileStorage{Path: acmeCfg.CertStorage},
	})

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
