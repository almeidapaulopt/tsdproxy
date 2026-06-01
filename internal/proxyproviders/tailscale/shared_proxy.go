// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package tailscale

import (
	"context"
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
	log           zerolog.Logger
	config        *model.Config
	shared        *SharedServer
	exposure      *SharedSNIExposure
	events        chan model.ProxyEvent
	forwarderDone chan struct{}
	stopCh        chan struct{}
	domain        string
	mtx           sync.RWMutex
	closeOnce     sync.Once
	started       bool
}

func (p *SharedProxy) Start(ctx context.Context) error {
	p.mtx.Lock()
	defer p.mtx.Unlock()

	if p.started {
		return nil
	}

	if err := p.exposure.Start(ctx, nil, p.config); err != nil {
		return err
	}

	p.started = true
	p.stopCh = make(chan struct{})
	p.forwarderDone = make(chan struct{})
	go p.forwardEvents()

	return nil
}

func (p *SharedProxy) Close() error {
	p.mtx.Lock()
	_ = p.exposure.Close(context.Background())
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
	p.mtx.RLock()
	defer p.mtx.RUnlock()

	_, ok := p.config.Ports[port]
	if !ok {
		return nil, ErrProxyPortNotFound
	}
	return p.exposure.GetListener(port)
}

func (p *SharedProxy) GetRawTCPListener(port string) (net.Listener, error) {
	p.mtx.RLock()
	defer p.mtx.RUnlock()

	_, ok := p.config.Ports[port]
	if !ok {
		return nil, ErrProxyPortNotFound
	}
	return p.exposure.getRawTCPListener(port)
}

func (p *SharedProxy) GetPacketConn(port string) (net.PacketConn, error) {
	p.mtx.RLock()
	defer p.mtx.RUnlock()

	_, ok := p.config.Ports[port]
	if !ok {
		return nil, ErrProxyPortNotFound
	}
	return p.exposure.getPacketConn(port)
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
