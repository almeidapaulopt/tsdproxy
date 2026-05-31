// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package tailscale

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"strings"
	"sync"

	"tailscale.com/client/local"

	"golang.org/x/sync/semaphore"

	"github.com/almeidapaulopt/tsdproxy/internal/model"
	"github.com/almeidapaulopt/tsdproxy/internal/tlsproviders"
)

// Provider implements tlsproviders.Provider using Tailscale's CertPair.
// Only works for MagicDNS domains (*.ts.net).
// The local client may be set lazily via SetLocalClient after creation.
type Provider struct {
	lc      *local.Client
	certSem *semaphore.Weighted
	mu      sync.RWMutex
}

var _ tlsproviders.Provider = (*Provider)(nil)

func New(lc *local.Client) *Provider {
	return &Provider{
		lc:      lc,
		certSem: semaphore.NewWeighted(model.DefaultMaxCertConcurrency),
	}
}

// SetLocalClient updates the Tailscale local client. Safe to call concurrently.
// Used when the TLS provider is created before the tsnet server is available.
func (p *Provider) SetLocalClient(lc *local.Client) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.lc = lc
}

func (p *Provider) getLocalClient() *local.Client {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.lc
}

func (p *Provider) Name() string {
	return model.TLSProviderTailscale
}

func (p *Provider) Provision(ctx context.Context, domain string) error {
	if !isMagicDNSDomain(domain) {
		return fmt.Errorf("tailscale tls: domain %q is not a MagicDNS domain", domain)
	}
	lc := p.getLocalClient()
	if lc == nil {
		return errors.New("tailscale tls: local client is nil")
	}
	if err := p.certSem.Acquire(ctx, 1); err != nil {
		return fmt.Errorf("tailscale tls: cert semaphore for %s: %w", domain, err)
	}
	defer p.certSem.Release(1)
	_, _, err := lc.CertPair(ctx, domain)
	if err != nil {
		return fmt.Errorf("tailscale tls: certpair for %s: %w", domain, err)
	}
	return nil
}

func (p *Provider) GetCertificate(ctx context.Context, domain string) (tls.Certificate, error) {
	lc := p.getLocalClient()
	if lc == nil {
		return tls.Certificate{}, errors.New("tailscale tls: local client is nil")
	}
	if err := p.certSem.Acquire(ctx, 1); err != nil {
		return tls.Certificate{}, fmt.Errorf("tailscale tls: cert semaphore for %s: %w", domain, err)
	}
	defer p.certSem.Release(1)
	certPEM, keyPEM, err := lc.CertPair(ctx, domain)
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
