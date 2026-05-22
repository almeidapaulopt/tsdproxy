// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package tailscale

import (
	"context"
	"net"
	"net/http"
	"strconv"
	"sync"

	"github.com/almeidapaulopt/tsdproxy/internal/model"
	"github.com/almeidapaulopt/tsdproxy/internal/proxyproviders"
	"github.com/rs/zerolog"
)

var (
	_ proxyproviders.ProxyInterface = (*SharedProxy)(nil)
	_ proxyproviders.RawTCPListener = (*SharedProxy)(nil)
)

// SharedProxy implements proxyproviders.ProxyInterface for proxies that share
// a single tsnet.Server via SNI routing.
type SharedProxy struct {
	log        zerolog.Logger
	config     *model.Config
	shared     *SharedServer
	domain     string
	vListeners map[string]*VirtualListener
	events     chan model.ProxyEvent
	closeOnce  sync.Once
	mtx        sync.RWMutex
	started    bool
}

func (p *SharedProxy) Start(ctx context.Context) error {
	p.mtx.Lock()
	defer p.mtx.Unlock()

	if p.started {
		return nil
	}

	if err := p.shared.Start(ctx); err != nil {
		return err
	}

	p.vListeners = make(map[string]*VirtualListener)

	for portName, portCfg := range p.config.Ports {
		if !p.needsSNI(&portCfg) {
			continue
		}

		vl, err := p.shared.Acquire(p.domain, portCfg.ProxyPort)
		if err != nil {
			for name, v := range p.vListeners {
				p.shared.Release(p.domain, p.config.Ports[name].ProxyPort)
				v.Close()
			}
			p.vListeners = nil
			return err
		}
		p.vListeners[portName] = vl
	}

	p.started = true

	go p.forwardEvents()

	return nil
}

func (p *SharedProxy) Close() error {
	p.mtx.Lock()
	defer p.mtx.Unlock()

	for portName, vl := range p.vListeners {
		if portCfg, ok := p.config.Ports[portName]; ok {
			p.shared.Release(p.domain, portCfg.ProxyPort)
		}
		vl.Close()
	}
	p.vListeners = nil

	p.closeOnce.Do(func() {
		close(p.events)
	})

	return nil
}

func (p *SharedProxy) GetListener(port string) (net.Listener, error) {
	p.mtx.RLock()
	vl, ok := p.vListeners[port]
	p.mtx.RUnlock()
	if ok {
		return vl, nil
	}

	portCfg, ok := p.config.Ports[port]
	if !ok {
		return nil, ErrProxyPortNotFound
	}

	addr := ":" + strconv.Itoa(portCfg.ProxyPort)
	return p.shared.tsServer.Listen("tcp", addr)
}

func (p *SharedProxy) GetRawTCPListener(port string) (net.Listener, error) {
	p.mtx.RLock()
	vl, ok := p.vListeners[port]
	p.mtx.RUnlock()

	if !ok {
		return nil, ErrProxyPortNotFound
	}
	return vl, nil
}

func (p *SharedProxy) GetPacketConn(port string) (net.PacketConn, error) {
	portCfg, ok := p.config.Ports[port]
	if !ok {
		return nil, ErrProxyPortNotFound
	}

	addr := ":" + strconv.Itoa(portCfg.ProxyPort)
	return p.shared.tsServer.ListenPacket("udp", addr)
}

func (p *SharedProxy) GetURL() string {
	scheme := p.primaryScheme()
	url := p.shared.GetURL()
	if url == "" {
		return ""
	}
	return scheme + "://" + url
}

func (p *SharedProxy) GetAuthURL() string {
	return ""
}

func (p *SharedProxy) WatchEvents() chan model.ProxyEvent {
	return p.events
}

func (p *SharedProxy) Whois(r *http.Request) model.Whois {
	return p.shared.Whois(r)
}

func (p *SharedProxy) primaryScheme() string {
	for _, port := range p.config.Ports {
		return port.ProxyProtocol
	}
	return model.ProtoHTTPS
}

func (p *SharedProxy) needsSNI(portCfg *model.PortConfig) bool {
	return portCfg.ProxyProtocol == model.ProtoHTTPS
}

func (p *SharedProxy) forwardEvents() {
	serverEvents := p.shared.SubscribeEvents()
	defer p.shared.UnsubscribeEvents(serverEvents)

	for evt := range serverEvents {
		select {
		case p.events <- evt:
		default:
			p.log.Warn().Msg("dropping proxy event: no listener")
		}
	}
}
