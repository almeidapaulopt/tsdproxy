// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package tailscale

import (
	"context"
	"errors"
	"net"
	"net/http"
	"sync"

	"github.com/rs/zerolog"

	"github.com/almeidapaulopt/tsdproxy/internal/model"
	"github.com/almeidapaulopt/tsdproxy/internal/proxyproviders"
)

var _ proxyproviders.ProxyInterface = (*ServiceProxy)(nil)

// ServiceProxy implements proxyproviders.ProxyInterface for proxies that use
// Tailscale VIP Services via a shared tsnet.Server.
type ServiceProxy struct {
	log         zerolog.Logger
	config      *model.Config
	services    *ServicesServer
	exposure    *ServicesVIPExposure
	events      chan model.ProxyEvent
	serviceName string
	fqdn        string
	mtx         sync.RWMutex
	closeOnce   sync.Once
	started     bool
}

func (p *ServiceProxy) Start(ctx context.Context) error {
	p.mtx.Lock()
	defer p.mtx.Unlock()

	if p.started {
		return nil
	}

	if err := p.exposure.Start(ctx, nil, p.config); err != nil {
		return err
	}

	p.fqdn = p.exposure.firstFQDN()
	p.started = true

	p.log.Info().
		Str("fqdn", p.fqdn).
		Msg("service proxy started")

	select {
	case p.events <- model.ProxyEvent{Status: model.ProxyStatusRunning}:
	default:
	}

	return nil
}

func (p *ServiceProxy) Close() error {
	p.mtx.Lock()
	_ = p.exposure.Close(context.Background())
	p.mtx.Unlock()

	p.log.Info().Msg("service proxy closed")

	p.closeOnce.Do(func() {
		close(p.events)
	})

	return nil
}

func (p *ServiceProxy) GetListener(port string) (net.Listener, error) {
	p.mtx.RLock()
	defer p.mtx.RUnlock()

	_, ok := p.config.Ports[port]
	if !ok {
		return nil, ErrProxyPortNotFound
	}
	return p.exposure.GetListener(port)
}

func (p *ServiceProxy) GetPacketConn(_ string) (net.PacketConn, error) {
	return nil, errors.New("services mode does not support UDP")
}

func (p *ServiceProxy) GetURL() string {
	p.mtx.RLock()
	fqdn := p.fqdn
	p.mtx.RUnlock()
	if fqdn == "" {
		return ""
	}
	scheme := primaryScheme(p.config.Ports)
	return scheme + "://" + fqdn
}

func (p *ServiceProxy) GetAuthURL() string {
	if p.services == nil {
		return ""
	}
	return p.services.GetAuthURL()
}

func (p *ServiceProxy) WatchEvents() chan model.ProxyEvent {
	return p.events
}

func (p *ServiceProxy) Whois(r *http.Request) model.Whois {
	return p.services.Whois(r)
}
