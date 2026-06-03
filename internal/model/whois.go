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

func IsLocalhost(remoteAddr string) bool {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func NormalizeIP(addr string) string {
	addr = strings.TrimSpace(addr)
	host, _, err := net.SplitHostPort(addr)
	if err == nil {
		if ip := net.ParseIP(host); ip != nil {
			return host
		}
		return ""
	}
	if ip := net.ParseIP(addr); ip != nil {
		return addr
	}
	return ""
}
