// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package tailscale

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"sync"
	"time"

	"github.com/almeidapaulopt/tsdproxy/internal/model"
	"github.com/almeidapaulopt/tsdproxy/internal/proxyproviders"

	"github.com/rs/zerolog"
	"golang.org/x/sync/semaphore"
	"tailscale.com/client/local"
	"tailscale.com/tsnet"
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
	if _, err := p.lifecycle.Start(ctx); err != nil {
		return err
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

	scheme := p.primaryScheme()
	return scheme + "://" + url
}

func (p *Proxy) GetLocalClient() *local.Client {
	rt := p.lifecycle.GetRuntime()
	if rt == nil {
		return nil
	}
	return rt.LocalClient
}

func (p *Proxy) primaryScheme() string {
	for _, port := range p.config.Ports {
		return port.ProxyProtocol
	}
	return model.ProtoHTTPS
}

func (p *Proxy) getStatus() model.ProxyStatus {
	p.mtx.RLock()
	s := p.status
	p.mtx.RUnlock()
	return s
}

// Close method implements proxyconfig.Proxy Close method.
func (p *Proxy) Close() error {
	p.mtx.RLock()
	wasStarted := p.started
	p.mtx.RUnlock()

	if !wasStarted {
		p.closeOnce.Do(func() {
			close(p.events)
		})
		return nil
	}

	err := p.lifecycle.Close()

	// Wait for bridgeEvents to finish (it returns when lifecycle closes its events channel).
	if p.bridgeDone != nil {
		<-p.bridgeDone
	}

	p.closeOnce.Do(func() {
		close(p.events)
	})

	return err
}

func (p *Proxy) GetListener(port string) (net.Listener, error) {
	portCfg, ok := p.config.Ports[port]
	if !ok {
		return nil, ErrProxyPortNotFound
	}

	ts := p.tsServer()
	if ts == nil {
		return nil, errors.New("proxy not started")
	}

	network := portCfg.ProxyProtocol
	if portCfg.ProxyProtocol == model.ProtoHTTP || portCfg.ProxyProtocol == model.ProtoHTTPS || portCfg.ProxyProtocol == model.ProtoUDP {
		network = "tcp"
	}
	addr := ":" + strconv.Itoa(portCfg.ProxyPort)

	if portCfg.Tailscale.Funnel {
		return ts.ListenFunnel(network, addr)
	}
	if portCfg.ProxyProtocol == model.ProtoHTTPS {
		return ts.ListenTLS(network, addr)
	}
	return ts.Listen(network, addr)
}

func (p *Proxy) GetRawTCPListener(port string) (net.Listener, error) {
	portCfg, ok := p.config.Ports[port]
	if !ok {
		return nil, ErrProxyPortNotFound
	}

	ts := p.tsServer()
	if ts == nil {
		return nil, errors.New("proxy not started")
	}

	addr := ":" + strconv.Itoa(portCfg.ProxyPort)
	return ts.Listen("tcp", addr)
}

func (p *Proxy) GetPacketConn(port string) (net.PacketConn, error) {
	portCfg, ok := p.config.Ports[port]
	if !ok {
		return nil, ErrProxyPortNotFound
	}

	ts := p.tsServer()
	if ts == nil {
		return nil, errors.New("proxy not started")
	}

	rt := p.lifecycle.GetRuntime()
	ip4, err := p.waitForIP(rt.Ctx)
	if err != nil {
		return nil, fmt.Errorf("cannot bind UDP port %d: %w", portCfg.ProxyPort, err)
	}

	addr := ip4.String() + ":" + strconv.Itoa(portCfg.ProxyPort)
	return ts.ListenPacket(model.ProtoUDP, addr)
}

func (p *Proxy) waitForIP(ctx context.Context) (netip.Addr, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	const (
		interval = 500 * time.Millisecond
		timeout  = 30 * time.Second
	)

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	ts := p.tsServer()
	for {
		ip4, _ := ts.TailscaleIPs()
		if ip4.IsValid() {
			return ip4, nil
		}

		select {
		case <-ctx.Done():
			return netip.Addr{}, errors.New("timed out waiting for tailscale IP")
		case <-ticker.C:
		}
	}
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
	return cachedWhoisFromAddr(p.whoisCache, rt.LocalClient, r.Context(), r.RemoteAddr)
}

// tsServer returns the tsnet.Server from the lifecycle runtime, or nil if not started.
func (p *Proxy) tsServer() *tsnet.Server {
	rt := p.lifecycle.GetRuntime()
	if rt == nil {
		return nil
	}
	return rt.Server
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
		case p.events <- model.ProxyEvent{Status: evt.Status}:
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
