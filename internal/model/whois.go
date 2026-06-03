// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package model

import (
	"context"
	"net"
	"strings"
)

type (
	Whois struct {
		ID            string
		DisplayName   string
		Username      string
		ProfilePicURL string
	}
)

func WhoisFromContext(ctx context.Context) (Whois, bool) {
	who, ok := ctx.Value(ContextKeyWhois).(Whois)

	return who, ok
}

func WhoisNewContext(ctx context.Context, who Whois) context.Context {
	return context.WithValue(ctx, ContextKeyWhois, who)
}

// IsLocalhost reports whether the given address (typically RemoteAddr in
// "ip:port" form) is a loopback address.
func IsLocalhost(remoteAddr string) bool {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// NormalizeIP takes an address in "ip:port" or bare IP form and returns
// the normalized IP string. It trims whitespace and strips any port
// component. Returns empty string if the input cannot be parsed.
func NormalizeIP(addr string) string {
	addr = strings.TrimSpace(addr)

	// Try splitting host:port first (handles "1.2.3.4:443" and "[::1]:443").
	host, _, err := net.SplitHostPort(addr)
	if err == nil {
		if ip := net.ParseIP(host); ip != nil {
			return host
		}
		return ""
	}

	// No port present — validate as a bare IP.
	if ip := net.ParseIP(addr); ip != nil {
		return addr
	}

	return ""
}
