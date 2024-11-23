// SPDX-FileCopyrightText: 2024 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT
package tailscale

import (
	"context"
	"net"

	"tailscale.com/tsnet"

	"github.com/almeidapaulopt/tsdproxy/internal/proxyconfig"
)

// Proxy struct implements proxyconfig.Proxy.
type Proxy struct {
	config   *proxyconfig.Config
	tsServer *tsnet.Server
}

// Start method implements proxyconfig.Proxy Start method.
func (p *Proxy) Start() error {
	// TODO: ADD status monitoring
	_, err := p.tsServer.Up(context.Background())
	return err
}

// Close method implements proxyconfig.Proxy Close method.
func (p *Proxy) Close() error {
	if p.tsServer != nil {
		return p.tsServer.Close()
	}
	return nil
}

// GetListener method implements proxyconfig.Proxy GetListener method.
func (p *Proxy) GetListener(network, addr string) (net.Listener, error) {
	return p.tsServer.Listen(network, addr)
}

// GetTLSListener method implements proxyconfig.Proxy GetTLSListener method.
func (p *Proxy) GetTLSListener(network, addr string) (net.Listener, error) {
	if p.config.Tailscale.Funnel {
		return p.tsServer.ListenFunnel(network, addr)
	}

	return p.tsServer.ListenTLS(network, addr)
}
