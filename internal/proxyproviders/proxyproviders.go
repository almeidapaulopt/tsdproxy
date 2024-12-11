// SPDX-FileCopyrightText: 2024 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT
package proxyproviders

import (
	"net"

	"github.com/almeidapaulopt/tsdproxy/internal/proxyconfig"
)

type (
	// Proxy interface for each proxy provider
	Provider interface {
		NewProxy(cfg *proxyconfig.Config) (Proxy, error)
	}

	// Proxy interface for each proxy
	Proxy interface {
		Start() error
		Close() error
		GetListener(network, addr string) (net.Listener, error)
		GetTLSListener(network, addr string) (net.Listener, error)
		GetURL() string
	}
)
