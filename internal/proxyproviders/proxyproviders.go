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
		NewProxy(cfg *model.Config) (ProxyInterface, error)
	}

	// ProxyInterface interface for each proxy
	ProxyInterface interface {
		Start(context.Context) error
		Close() error
		GetListener(port string) (net.Listener, error)
		GetURL() string
		GetAuthURL() string
		WatchEvents() chan model.ProxyEvent
		Whois(r *http.Request) model.Whois
	}
)
