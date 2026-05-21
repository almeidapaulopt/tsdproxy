// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package proxyproviders

import (
	"context"
	"net"
	"net/http"

	"github.com/almeidapaulopt/tsdproxy/internal/model"
)

type (
	// Proxy interface for each proxy provider
	Provider interface {
		// ResolveAuthKey resolves the authentication key for the given config
		// (e.g. OAuth token exchange). Side-effect-free with respect to local
		// state. Call before closing an existing proxy so network/auth failures
		// don't tear down a working proxy.
		ResolveAuthKey(cfg *model.Config) (string, error)
		NewProxy(cfg *model.Config) (ProxyInterface, error)
	}

	// ProxyInterface interface for each proxy
	ProxyInterface interface {
		Start(context.Context) error
		Close() error
		GetListener(port string) (net.Listener, error)
		GetPacketConn(port string) (net.PacketConn, error)
		GetURL() string
		GetAuthURL() string
		WatchEvents() chan model.ProxyEvent
		Whois(r *http.Request) model.Whois
	}
)
