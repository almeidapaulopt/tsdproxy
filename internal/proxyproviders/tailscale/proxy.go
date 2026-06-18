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
	"time"

	"github.com/almeidapaulopt/tsdproxy/internal/model"
	"github.com/almeidapaulopt/tsdproxy/internal/proxyproviders"

	"github.com/rs/zerolog"
	"golang.org/x/sync/semaphore"
	"tailscale.com/client/local"
)

// whoisCacheTTL is how long a successful Whois result is cached before
// requiring a fresh lookup from tailscaled.
const whoisCacheTTL = 30 * time.Second

const whoisCacheMaxEntries = 1024

// Proxy struct implements proxyconfig.Proxy.
// It composes a NodeLifecycle internally and bridges NodeEvents to ProxyEvents.
type Proxy struct {
	log        zerolog.Logger
	config     *model.Config
	certSem    *semaphore.Weighted
	lifecycle  *NodeLifecycle
	exposure   *PerProxyExposure
	events     chan model.ProxyEvent
	bridgeDone chan struct{}
	whoisCache *WhoisCache
	authURL    string
	url        string
	status     model.ProxyStatus
	mtx        sync.RWMutex
	closeOnce  sync.Once
	started    bool
}

var (
	_ proxyproviders.ProxyInterface = (*Proxy)(nil)
	_ proxyproviders.RawTCPListener = (*Proxy)(nil)

	ErrProxyPortNotFound = errors.New("proxy port not found")
)

// Start method implements proxyconfig.Proxy Start method.
func (p *Proxy) Start(ctx context.Context) error {
	rt, err := p.lifecycle.Start(ctx)
	if err != nil {
		return err
	}

	if err := p.exposure.Start(ctx, rt, p.config); err != nil {
		if closeErr := p.lifecycle.Close(); closeErr != nil {
			p.log.Error().Err(closeErr).Msg("failed to close lifecycle")
		}
		return fmt.Errorf("start traffic exposure: %w", err)
	}

	p.bridgeDone = make(chan struct{})
	go p.bridgeEvents()

	p.mtx.Lock()
	p.started = true
	p.mtx.Unlock()

	return nil
}

func (p *Proxy) GetURL() string {
	p.mtx.RLock()
	url := p.url
	p.mtx.RUnlock()

	if url == "" {
		return ""
	}
	scheme := primaryScheme(p.config.Ports)
	return scheme + "://" + url
}

func (p *Proxy) GetLocalClient() *local.Client {
	rt := p.lifecycle.GetRuntime()
	if rt == nil {
		return nil
	}
	return rt.LocalClient
}

// Close method implements proxyconfig.Proxy Close method.
func (p *Proxy) Close() error {
	p.mtx.Lock()
	if !p.started {
		p.mtx.Unlock()
		p.closeOnce.Do(func() {
			close(p.events)
		})
		return nil
	}
	p.mtx.Unlock()

	if err := p.exposure.Close(context.Background()); err != nil {
		p.log.Error().Err(err).Msg("failed to close exposure")
	}
	err := p.lifecycle.Close()

	// Wait for bridgeEvents to finish (it returns when lifecycle closes its events channel).
	if p.bridgeDone != nil {
		<-p.bridgeDone
	}

	p.mtx.Lock()
	p.started = false
	p.mtx.Unlock()

	p.closeOnce.Do(func() {
		close(p.events)
	})

	return err
}

func (p *Proxy) GetListener(port string) (net.Listener, error) {
	p.mtx.RLock()
	defer p.mtx.RUnlock()

	_, ok := p.config.Ports[port]
	if !ok {
		return nil, ErrProxyPortNotFound
	}
	return p.exposure.GetListener(port)
}

func (p *Proxy) GetRawTCPListener(port string) (net.Listener, error) {
	p.mtx.RLock()
	defer p.mtx.RUnlock()

	_, ok := p.config.Ports[port]
	if !ok {
		return nil, ErrProxyPortNotFound
	}
	return p.exposure.GetRawTCPListener(port)
}

func (p *Proxy) GetPacketConn(port string) (net.PacketConn, error) {
	p.mtx.RLock()
	defer p.mtx.RUnlock()

	_, ok := p.config.Ports[port]
	if !ok {
		return nil, ErrProxyPortNotFound
	}
	return p.exposure.GetPacketConn(port)
}

func (p *Proxy) WatchEvents() chan model.ProxyEvent {
	return p.events
}

func (p *Proxy) GetAuthURL() string {
	p.mtx.RLock()
	authURL := p.authURL
	p.mtx.RUnlock()
	return authURL
}

func (p *Proxy) Whois(r *http.Request) model.Whois {
	if r == nil {
		return model.Whois{}
	}
	rt := p.lifecycle.GetRuntime()
	if rt == nil {
		return model.Whois{}
	}
	return cachedWhoisFromAddr(r.Context(), p.whoisCache, rt.LocalClient, r.RemoteAddr)
}

// bridgeEvents reads from lifecycle NodeEvents and forwards them as ProxyEvents.
// It handles deduplication, cert prefetch on Running transitions, and returns
// when the lifecycle's events channel is closed.
func (p *Proxy) bridgeEvents() {
	defer close(p.bridgeDone)

	lifecycleEvents := p.lifecycle.WatchEvents()
	for evt := range lifecycleEvents {
		p.mtx.Lock()
		if p.status == evt.Status && p.url == evt.URL && p.authURL == evt.AuthURL {
			p.mtx.Unlock()
			continue
		}
		p.log.Debug().Str("status", evt.Status.String()).Msg("tailscale status")

		prevStatus := p.status
		p.status = evt.Status
		if evt.URL != "" {
			p.url = evt.URL
		}
		if evt.AuthURL != "" {
			p.authURL = evt.AuthURL
		}
		p.mtx.Unlock()

		// Trigger TLS cert prefetch on transition to Running with HTTPS ports.
		if evt.Status == model.ProxyStatusRunning && prevStatus != model.ProxyStatusRunning && p.hasHTTPSPort() {
			go p.getTLSCertificates()
		}

		select {
		case p.events <- model.ProxyEvent{Status: evt.Status, ErrorMessage: evt.ErrorMessage, AuthURL: evt.AuthURL}:
		default:
			p.log.Warn().Msg("dropping proxy event: no listener")
		}
	}
}

func (p *Proxy) getTLSCertificates() {
	rt := p.lifecycle.GetRuntime()
	if rt == nil {
		return
	}
	acquireCert(rt.Ctx, rt.LocalClient, rt.Server, p.certSem, p.log)
}

func (p *Proxy) hasHTTPSPort() bool {
	for _, port := range p.config.Ports {
		if port.ProxyProtocol == model.ProtoHTTPS {
			return true
		}
	}
	return false
}
