// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package proxymanager

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"strings"

	"tailscale.com/client/local"

	"github.com/almeidapaulopt/tsdproxy/internal/proxyproviders"
	tsproxy "github.com/almeidapaulopt/tsdproxy/internal/proxyproviders/tailscale"
	"github.com/almeidapaulopt/tsdproxy/internal/tlsproviders"
)

func (proxy *Proxy) getCustomTLSListener(portName string) (net.Listener, error) {
	raw, ok := proxy.providerProxy.(proxyproviders.RawTCPListener)
	if !ok {
		return nil, errors.New("custom domain TLS requires raw TCP listener support from proxy provider")
	}

	l, err := raw.GetRawTCPListener(portName)
	if err != nil {
		return nil, fmt.Errorf("get raw tcp listener: %w", err)
	}

	domain := proxy.Config.Domain
	tlsProv := proxy.tlsProvider

	return tls.NewListener(l, &tls.Config{
		MinVersion: tls.VersionTLS12,
		GetCertificate: func(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
			proxy.mtx.RLock()
			tlsActive := proxy.tlsStatus == tlsproviders.TLSStatusActive
			proxy.mtx.RUnlock()

			if tlsActive {
				cert, err := tlsProv.GetCertificate(hello.Context(), domain)
				if err == nil {
					return &cert, nil
				}
				proxy.log.Warn().Err(err).Msg("custom cert lookup failed, falling back to Tailscale cert")
			}

			// Fallback to Tailscale automatic cert
			cert, err := proxy.getTailscaleCertificate(hello.Context())
			if err != nil {
				return nil, fmt.Errorf("no certificate available for %s: %w", domain, err)
			}
			return cert, nil
		},
	}), nil
}

func (proxy *Proxy) getTailscaleCertificate(ctx context.Context) (*tls.Certificate, error) {
	lcGetter, ok := proxy.providerProxy.(interface{ GetLocalClient() *local.Client })
	if !ok {
		return nil, errors.New("tailscale cert not available: provider does not support GetLocalClient")
	}
	lc := lcGetter.GetLocalClient()
	if lc == nil {
		return nil, errors.New("tailscale local client not available")
	}

	rawURL := proxy.providerProxy.GetURL()
	hostname := strings.TrimPrefix(rawURL, "https://")
	hostname = strings.TrimPrefix(hostname, "http://")
	if hostname == "" {
		return nil, errors.New("tailscale hostname not yet available")
	}

	return tsproxy.CertPairToTLSCertificate(ctx, lc, hostname)
}
