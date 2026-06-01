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

const (
	certTimeout     = 5 * time.Minute
	certRetryInit   = 10 * time.Second
	certRetryMax    = 5 * time.Minute
	certMaxAttempts = 6
)

// acquireCert provisions a TLS certificate via CertPair with retry + backoff
// for transient failures (rate limits, timeouts).
func acquireCert(ctx context.Context, lc *local.Client, tsServer *tsnet.Server, sem *semaphore.Weighted, log zerolog.Logger) {
	if lc == nil || tsServer == nil || sem == nil {
		return
	}

	certDomains := tsServer.CertDomains()
	if len(certDomains) == 0 {
		log.Warn().Msg("no certificate domains available")
		return
	}
	domain := certDomains[0]

	backoff := certRetryInit
	for attempt := 0; attempt < certMaxAttempts; attempt++ {
		certCtx, cancel := context.WithTimeout(ctx, certTimeout)

		waitStart := time.Now()
		if err := sem.Acquire(certCtx, 1); err != nil {
			cancel()
			if !errors.Is(err, context.Canceled) {
				log.Error().Err(err).Msg("failed to acquire cert semaphore")
			}
			return
		}

		if wait := time.Since(waitStart); wait > time.Second {
			log.Warn().Dur("wait", wait).Msg("cert generation delayed by semaphore contention")
		}

		log.Info().Int("attempt", attempt+1).Msg("Generating TLS certificate")
		// Use a closure to guarantee sem.Release(1) on all exit paths,
		// including panics from lc.CertPair(). Without this defer, a
		// panic would permanently leak the semaphore token, eventually
		// deadlocking all concurrent cert acquisition.
		err := func() error {
			defer sem.Release(1)
			defer cancel()
			_, _, e := lc.CertPair(certCtx, domain)
			return e
		}()

		if err == nil {
			log.Info().Msg("TLS certificate generated")
			return
		}

		if errors.Is(err, context.Canceled) {
			return
		}

		log.Warn().Err(err).Int("attempt", attempt+1).Dur("backoff", backoff).
			Msg("cert generation failed, retrying")

		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}

		backoff *= 2
		if backoff > certRetryMax {
			backoff = certRetryMax
		}
	}

	log.Error().Int("maxAttempts", certMaxAttempts).Msg("cert generation failed after max attempts")
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
