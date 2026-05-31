// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package model

import (
	"context"
	"net"
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
