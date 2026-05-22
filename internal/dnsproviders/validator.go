// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package dnsproviders

import (
	"context"
	"fmt"
	"net"
	"strings"
	"time"

	dns "codeberg.org/miekg/dns"
)

const (
	defaultDNSAddr        = "8.8.8.8:53"
	defaultDNSReadTimeout = 5 * time.Second
)

func ValidateCNAME(ctx context.Context, domain, expectedTarget string) (bool, error) {
	return validateCNAMEWithAddr(ctx, domain, expectedTarget, defaultDNSAddr)
}

func validateCNAMEWithAddr(ctx context.Context, domain, expectedTarget, dnsAddr string) (bool, error) {
	m := dns.NewMsg(domain, dns.TypeCNAME)
	if m == nil {
		return false, fmt.Errorf("invalid domain %q", domain)
	}

	c := dns.NewClient()
	t := dns.NewTransport()
	t.Dialer = &net.Dialer{Timeout: defaultDNSReadTimeout}
	t.ReadTimeout = defaultDNSReadTimeout
	c.Transport = t

	r, _, err := c.Exchange(ctx, m, "udp", dnsAddr)
	if err != nil {
		return false, fmt.Errorf("dns query for %s: %w", domain, err)
	}

	target := ensureTrailingDot(expectedTarget)
	for _, rr := range r.Answer {
		if cn, ok := rr.(*dns.CNAME); ok {
			if strings.EqualFold(cn.Target, target) {
				return true, nil
			}
		}
	}

	return false, nil
}

func PollCNAME(ctx context.Context, domain, expectedTarget string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(2 * time.Second) //nolint:mnd
	defer ticker.Stop()

	for {
		ok, err := ValidateCNAME(ctx, domain, expectedTarget)
		if err != nil {
			return fmt.Errorf("validate cname: %w", err)
		}
		if ok {
			return nil
		}

		select {
		case <-ctx.Done():
			return fmt.Errorf("cname propagation timeout for %s: %w", domain, ctx.Err())
		case <-ticker.C:
		}
	}
}

func ensureTrailingDot(s string) string {
	if s != "" && !strings.HasSuffix(s, ".") {
		return s + "."
	}
	return s
}
