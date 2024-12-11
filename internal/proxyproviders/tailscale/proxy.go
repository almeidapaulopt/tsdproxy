// SPDX-FileCopyrightText: 2024 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT
package tailscale

import (
	"context"
	"net"
	"strings"

	"github.com/almeidapaulopt/tsdproxy/internal/proxyconfig"

	"github.com/rs/zerolog"
	"tailscale.com/client/tailscale"
	"tailscale.com/ipn"
	"tailscale.com/ipn/ipnstate"
	"tailscale.com/tsnet"
)

// Proxy struct implements proxyconfig.Proxy.
type Proxy struct {
	log      zerolog.Logger
	config   *proxyconfig.Config
	tsServer *tsnet.Server
	status   *ipnstate.Status
	lc       *tailscale.LocalClient
}

// Start method implements proxyconfig.Proxy Start method.
func (p *Proxy) Start() error {
	var err error
	ctx := context.Background()

	p.lc, err = p.tsServer.LocalClient()
	if err != nil {
		return err
	}

	go p.watchStatus(ctx)

	_, err = p.tsServer.Up(ctx)
	if err != nil {
		return err
	}

	return nil
}

func (p *Proxy) GetURL() string {
	if p.status == nil {
		return ""
	}

	// TODO: should be configurable and not force to https
	return "https://" + strings.TrimRight(p.status.Self.DNSName, ".")
}

func (p *Proxy) watchStatus(ctx context.Context) {
	watcher, err := p.lc.WatchIPNBus(ctx, ipn.NotifyInitialState)
	if err != nil {
		p.log.Error().Err(err).Msg("tailscale.watchStatus")
		return
	}
	defer watcher.Close()

	for {
		n, err := watcher.Next()
		if err != nil {
			p.log.Error().Err(err).Msg("tailscale.watchStatus")
			return
		}

		if n.ErrMessage != nil {
			p.log.Error().Err(err).Msg("tailscale.watchStatus: backend")
			return
		}

		if s := n.State; s != nil {
			status, err := p.lc.Status(ctx)
			if err != nil {
				p.log.Error().Err(err).Msg("tailscale.watchStatus")
				return
			}

			p.status = status
		}
	}
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
