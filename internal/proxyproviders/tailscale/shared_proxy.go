// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package tailscale

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync"

	"github.com/rs/zerolog"
	"tailscale.com/client/local"

	"github.com/almeidapaulopt/tsdproxy/internal/model"
	"github.com/almeidapaulopt/tsdproxy/internal/proxyproviders"
)

var (
	_ proxyproviders.ProxyInterface = (*SharedProxy)(nil)
	_ proxyproviders.RawTCPListener = (*SharedProxy)(nil)
)

// SharedProxy implements proxyproviders.ProxyInterface for proxies that share
// a single tsnet.Server via port-based multiplexing (SNI, HTTP Host, or direct).
type SharedProxy struct {
	log             zerolog.Logger
	config          *model.Config
	shared          *SharedServer
	vListeners      map[string]*VirtualListener
	directListeners map[string]net.Listener
	packetConns     map[string]net.PacketConn
	events          chan model.ProxyEvent
	forwarderDone   chan struct{}
	stopCh          chan struct{}
	domain          string
	mtx             sync.RWMutex
	closeOnce       sync.Once
	started         bool
}

func (p *SharedProxy) Start(ctx context.Context) error {
	p.mtx.Lock()
	defer p.mtx.Unlock()

	if p.started {
		return nil
	}

	p.vListeners = make(map[string]*VirtualListener)
	p.directListeners = make(map[string]net.Listener)

	for portName, portCfg := range p.config.Ports {
		select {
		case <-ctx.Done():
			p.rollbackAcquired()
			return ctx.Err()
		default:
		}
		switch portCfg.ProxyProtocol {
		case model.ProtoHTTPS, model.ProtoHTTP:
			vl, _, err := p.shared.Acquire(p.domain, portCfg.ProxyPort, portCfg.ProxyProtocol)
			if err != nil {
				p.rollbackAcquired()
				return err
			}
			p.vListeners[portName] = vl

		case model.ProtoTCP:
			_, direct, err := p.shared.Acquire(p.domain, portCfg.ProxyPort, portCfg.ProxyProtocol)
			if err != nil {
				p.rollbackAcquired()
				return err
			}
			p.directListeners[portName] = direct

		case model.ProtoUDP:
			pc, err := p.shared.AcquirePacket(p.domain, portCfg.ProxyPort)
			if err != nil {
				p.rollbackAcquired()
				return err
			}
			if p.packetConns == nil {
				p.packetConns = make(map[string]net.PacketConn)
			}
			p.packetConns[portName] = pc
		}
	}

	p.started = true
	p.stopCh = make(chan struct{})
	p.forwarderDone = make(chan struct{})
	go p.forwardEvents()

	return nil
}

// releaseAllPorts releases all acquired routes and closes all listeners.
// Must be called with p.mtx held.
func (p *SharedProxy) releaseAllPorts() {
	for name, vl := range p.vListeners {
		if portCfg, ok := p.config.Ports[name]; ok {
			p.shared.Release(p.domain, portCfg.ProxyPort, portCfg.ProxyProtocol)
		}
		vl.Close()
	}
	for name, l := range p.directListeners {
		if portCfg, ok := p.config.Ports[name]; ok {
			p.shared.Release(p.domain, portCfg.ProxyPort, portCfg.ProxyProtocol)
		}
		l.Close()
	}
	for name, pc := range p.packetConns {
		if portCfg, ok := p.config.Ports[name]; ok {
			p.shared.ReleasePacket(p.domain, portCfg.ProxyPort)
		}
		_ = pc // already closed by ReleasePacket → unregisterPacketRoute
	}
	p.vListeners = nil
	p.directListeners = nil
	p.packetConns = nil
}

func (p *SharedProxy) rollbackAcquired() {
	p.releaseAllPorts()
}

func (p *SharedProxy) Close() error {
	p.mtx.Lock()
	p.releaseAllPorts()
	if p.stopCh != nil {
		close(p.stopCh)
		p.stopCh = nil
	}
	p.mtx.Unlock()

	if p.forwarderDone != nil {
		<-p.forwarderDone
	}

	p.closeOnce.Do(func() {
		close(p.events)
	})

	return nil
}

func (p *SharedProxy) GetListener(port string) (net.Listener, error) {
	portCfg, ok := p.config.Ports[port]
	if !ok {
		return nil, ErrProxyPortNotFound
	}

	switch portCfg.ProxyProtocol {
	case model.ProtoHTTPS:
		p.mtx.RLock()
		vl, ok := p.vListeners[port]
		p.mtx.RUnlock()
		if !ok {
			return nil, ErrProxyPortNotFound
		}
		return tls.NewListener(vl, &tls.Config{
			MinVersion: tls.VersionTLS12,
			GetCertificate: func(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
				if hello.ServerName != "" && hello.ServerName != p.domain {
					return nil, fmt.Errorf("SNI mismatch: got %q, want %q", hello.ServerName, p.domain)
				}
				lc := p.shared.GetLocalClient()
				if lc == nil {
					return nil, errors.New("shared server local client not available")
				}
				return CertPairToTLSCertificate(hello.Context(), lc, p.domain)
			},
		}), nil

	case model.ProtoHTTP:
		p.mtx.RLock()
		vl, ok := p.vListeners[port]
		p.mtx.RUnlock()
		if !ok {
			return nil, ErrProxyPortNotFound
		}
		return vl, nil

	default:
		return nil, ErrProxyPortNotFound
	}
}

func (p *SharedProxy) GetRawTCPListener(port string) (net.Listener, error) {
	p.mtx.RLock()
	l, ok := p.directListeners[port]
	if ok {
		p.mtx.RUnlock()
		return l, nil
	}
	vl, ok := p.vListeners[port]
	p.mtx.RUnlock()
	if !ok {
		return nil, ErrProxyPortNotFound
	}
	return vl, nil
}

func (p *SharedProxy) GetPacketConn(port string) (net.PacketConn, error) {
	p.mtx.RLock()
	pc, ok := p.packetConns[port]
	p.mtx.RUnlock()
	if !ok {
		return nil, ErrProxyPortNotFound
	}
	return pc, nil
}

func (p *SharedProxy) GetURL() string {
	scheme := primaryScheme(p.config.Ports)
	url := p.shared.GetURL()
	if url == "" {
		return ""
	}
	return scheme + "://" + url
}

func (p *SharedProxy) GetAuthURL() string {
	if p.shared == nil {
		return ""
	}
	return p.shared.GetAuthURL()
}

func (p *SharedProxy) WatchEvents() chan model.ProxyEvent {
	return p.events
}

func (p *SharedProxy) Whois(r *http.Request) model.Whois {
	return p.shared.Whois(r)
}

func (p *SharedProxy) GetLocalClient() *local.Client {
	return p.shared.GetLocalClient()
}

func (p *SharedProxy) forwardEvents() {
	defer close(p.forwarderDone)

	serverEvents := p.shared.SubscribeEvents()
	if serverEvents == nil {
		return
	}
	defer p.shared.UnsubscribeEvents(serverEvents)

	for {
		select {
		case evt, ok := <-serverEvents:
			if !ok {
				return
			}
			select {
			case p.events <- evt:
			default:
				p.log.Warn().Msg("dropping proxy event: no listener")
			}
		case <-p.stopCh:
			return
		}
	}
}
