// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package tailscale

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"time"

	"github.com/rs/zerolog"
	"golang.org/x/sync/semaphore"
	"tailscale.com/client/local"
	"tailscale.com/tsnet"
)

const certTimeout = 2 * time.Minute

// acquireCert acquires the semaphore and provisions a TLS certificate via
// CertPair for the first domain returned by tsServer.CertDomains().
func acquireCert(ctx context.Context, lc *local.Client, tsServer *tsnet.Server, sem *semaphore.Weighted, log zerolog.Logger) {
	if lc == nil || tsServer == nil || sem == nil {
		return
	}

	certCtx, cancel := context.WithTimeout(ctx, certTimeout)
	defer cancel()

	waitStart := time.Now()
	if err := sem.Acquire(certCtx, 1); err != nil {
		if !errors.Is(err, context.Canceled) {
			log.Error().Err(err).Msg("failed to acquire cert semaphore")
		}
		return
	}
	defer sem.Release(1)

	if wait := time.Since(waitStart); wait > time.Second {
		log.Warn().Dur("wait", wait).Msg("cert generation delayed by semaphore contention")
	}

	log.Info().Msg("Generating TLS certificate")
	certDomains := tsServer.CertDomains()
	if len(certDomains) == 0 {
		log.Warn().Msg("no certificate domains available")
		return
	}

	if _, _, err := lc.CertPair(certCtx, certDomains[0]); err != nil {
		if !errors.Is(err, context.Canceled) {
			log.Error().Err(err).Msg("error getting TLS certificates")
		}
		return
	}
	log.Info().Msg("TLS certificate generated")
}

// CertPairToTLSCertificate calls CertPair on the local client and wraps the
// PEM blocks into a tls.Certificate. Shared by per-proxy and shared-proxy paths.
func CertPairToTLSCertificate(ctx context.Context, lc *local.Client, domain string) (*tls.Certificate, error) {
	certPEM, keyPEM, err := lc.CertPair(ctx, domain)
	if err != nil {
		return nil, fmt.Errorf("tailscale CertPair for %s: %w", domain, err)
	}
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return nil, fmt.Errorf("parse cert for %s: %w", domain, err)
	}
	return &cert, nil
}
