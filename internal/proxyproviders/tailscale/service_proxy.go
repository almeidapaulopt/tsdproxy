// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package tailscale

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync"

	"github.com/rs/zerolog"
	"tailscale.com/tsnet"

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
	listeners   map[string]*tsnet.ServiceListener
	events      chan model.ProxyEvent
	serviceName string
	fqdn        string
	mtx         sync.RWMutex
	closeOnce   sync.Once
	started     bool
}

func (p *ServiceProxy) Start(_ context.Context) error {
	p.mtx.Lock()
	defer p.mtx.Unlock()

	if p.started {
		return nil
	}

	p.listeners = make(map[string]*tsnet.ServiceListener)

	for portName, portCfg := range p.config.Ports {
		var (
			listener *tsnet.ServiceListener
			err      error
		)

		switch portCfg.ProxyProtocol {
		case model.ProtoHTTPS:
			listener, err = p.services.Acquire(p.serviceName, uint16(portCfg.ProxyPort), true, false) //nolint:gosec // port limits validated in config
		case model.ProtoHTTP:
			listener, err = p.services.Acquire(p.serviceName, uint16(portCfg.ProxyPort), false, false) //nolint:gosec // port limits validated in config
		case model.ProtoTCP:
			listener, err = p.services.Acquire(p.serviceName, uint16(portCfg.ProxyPort), false, true) //nolint:gosec // port limits validated in config
		default:
			p.rollbackAcquired()
			return fmt.Errorf("services mode does not support protocol %q", portCfg.ProxyProtocol)
		}

		if err != nil {
			p.rollbackAcquired()
			return err
		}

		p.listeners[portName] = listener

		if p.fqdn == "" {
			p.fqdn = listener.FQDN
		}
	}

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

func (p *ServiceProxy) rollbackAcquired() {
	for portName := range p.listeners {
		if portCfg, ok := p.config.Ports[portName]; ok {
			if err := p.services.Release(p.serviceName, uint16(portCfg.ProxyPort)); err != nil { //nolint:gosec // port limits validated in config
				p.log.Warn().Err(err).Int("port", portCfg.ProxyPort).Msg("failed to release service during rollback")
			}
		}
	}
	p.listeners = nil
}

func (p *ServiceProxy) Close() error {
	p.mtx.Lock()
	for portName := range p.listeners {
		if portCfg, ok := p.config.Ports[portName]; ok {
			if err := p.services.Release(p.serviceName, uint16(portCfg.ProxyPort)); err != nil { //nolint:gosec // port limits validated in config
				p.log.Warn().Err(err).Int("port", portCfg.ProxyPort).Msg("failed to release service")
			}
		}
	}
	p.listeners = nil
	p.mtx.Unlock()

	p.log.Info().Msg("service proxy closed")

	p.closeOnce.Do(func() {
		close(p.events)
	})

	return nil
}

func (p *ServiceProxy) GetListener(port string) (net.Listener, error) {
	p.mtx.RLock()
	sl, ok := p.listeners[port]
	p.mtx.RUnlock()

	if !ok {
		return nil, ErrProxyPortNotFound
	}
	return sl, nil
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
