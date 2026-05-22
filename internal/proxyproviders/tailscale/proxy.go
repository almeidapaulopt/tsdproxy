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
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/almeidapaulopt/tsdproxy/internal/model"
	"github.com/almeidapaulopt/tsdproxy/internal/proxyproviders"

	"github.com/rs/zerolog"
	"golang.org/x/sync/semaphore"
	"tailscale.com/client/local"
	"tailscale.com/ipn"
	"tailscale.com/tsnet"
)

// Proxy struct implements proxyconfig.Proxy.
type Proxy struct {
	log       zerolog.Logger
	ctx       context.Context
	watchDone chan struct{}
	config    *model.Config
	tsServer  *tsnet.Server
	lc        *local.Client
	events    chan model.ProxyEvent
	certSem   *semaphore.Weighted
	authURL   string
	url       string
	status    model.ProxyStatus
	closeOnce sync.Once
	watchOnce sync.Once
	mtx       sync.RWMutex
	started   bool
}

var (
	_ proxyproviders.ProxyInterface   = (*Proxy)(nil)
	_ proxyproviders.RawTCPListener   = (*Proxy)(nil)

	ErrProxyPortNotFound = errors.New("proxy port not found")
)

// Start method implements proxyconfig.Proxy Start method.
func (p *Proxy) Start(ctx context.Context) error {
	var (
		err error
		lc  *local.Client
	)

	if err = p.tsServer.Start(); err != nil {
		return err
	}

	p.mtx.Lock()
	p.started = true
	p.mtx.Unlock()

	if lc, err = p.tsServer.LocalClient(); err != nil {
		return err
	}

	p.mtx.Lock()
	p.ctx = ctx
	p.lc = lc
	p.mtx.Unlock()

	p.watchOnce.Do(func() {
		p.mtx.Lock()
		p.watchDone = make(chan struct{})
		p.mtx.Unlock()

		go p.watchStatus()
	})

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
	p.mtx.RLock()
	defer p.mtx.RUnlock()
	return p.lc
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
	watchDone := p.watchDone
	p.mtx.RUnlock()

	if !wasStarted {
		p.closeOnce.Do(func() {
			close(p.events)
		})
		return nil
	}

	var err error
	if p.tsServer != nil {
		err = p.tsServer.Close()

		if p.config.Tailscale.Ephemeral && p.tsServer.Dir != "" {
			if removeErr := os.RemoveAll(p.tsServer.Dir); removeErr != nil {
				p.log.Error().Err(removeErr).Msg("failed to clean up ephemeral node state")
			}
		}
	}

	if watchDone != nil {
		<-watchDone
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

	network := portCfg.ProxyProtocol
	if portCfg.ProxyProtocol == "http" || portCfg.ProxyProtocol == "https" || portCfg.ProxyProtocol == "udp" {
		network = "tcp"
	}
	addr := ":" + strconv.Itoa(portCfg.ProxyPort)

	if portCfg.Tailscale.Funnel {
		return p.tsServer.ListenFunnel(network, addr)
	}
	if portCfg.ProxyProtocol == "https" {
		return p.tsServer.ListenTLS(network, addr)
	}
	return p.tsServer.Listen(network, addr)
}

func (p *Proxy) GetRawTCPListener(port string) (net.Listener, error) {
	portCfg, ok := p.config.Ports[port]
	if !ok {
		return nil, ErrProxyPortNotFound
	}

	addr := ":" + strconv.Itoa(portCfg.ProxyPort)
	return p.tsServer.Listen("tcp", addr)
}

func (p *Proxy) GetPacketConn(port string) (net.PacketConn, error) {
	portCfg, ok := p.config.Ports[port]
	if !ok {
		return nil, ErrProxyPortNotFound
	}

	ip4, err := p.waitForIP(p.ctx)
	if err != nil {
		return nil, fmt.Errorf("cannot bind UDP port %d: %w", portCfg.ProxyPort, err)
	}

	addr := ip4.String() + ":" + strconv.Itoa(portCfg.ProxyPort)
	return p.tsServer.ListenPacket("udp", addr)
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

	for {
		ip4, _ := p.tsServer.TailscaleIPs()
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
	p.mtx.RLock()
	lc := p.lc
	p.mtx.RUnlock()
	if lc == nil {
		return model.Whois{}
	}
	who, err := lc.WhoIs(r.Context(), r.RemoteAddr)
	if err != nil {
		return model.Whois{}
	}

	if who.UserProfile == nil {
		return model.Whois{}
	}

	// Reject tagged nodes — their UserProfile is the pseudo-user
	// "tagged-devices", not a real user identity. Without this check,
	// any tagged container in the tailnet could spoof as a user and
	// call admin endpoints or access allowlist-gated proxies.
	if who.Node != nil && who.Node.IsTagged() {
		return model.Whois{}
	}

	return model.Whois{
		DisplayName:   who.UserProfile.DisplayName,
		Username:      who.UserProfile.LoginName,
		ID:            who.UserProfile.ID.String(),
		ProfilePicURL: who.UserProfile.ProfilePicURL,
	}
}

func (p *Proxy) watchStatus() {
	defer func() {
		p.mtx.Lock()
		if p.watchDone != nil {
			close(p.watchDone)
		}
		p.mtx.Unlock()
	}()

	p.mtx.Lock()
	lc := p.lc
	p.mtx.Unlock()
	if lc == nil {
		p.log.Error().Msg("tailscale.watchStatus: local client is nil")
		return
	}

	watcher, err := lc.WatchIPNBus(p.ctx, ipn.NotifyInitialState|ipn.NotifyNoPrivateKeys|ipn.NotifyInitialHealthState)
	if err != nil {
		p.log.Error().Err(err).Msg("tailscale.watchStatus")
		return
	}
	defer watcher.Close()

	for {
		n, err := watcher.Next()
		if err != nil {
			if !errors.Is(err, context.Canceled) {
				p.log.Error().Err(err).Msg("tailscale.watchStatus: Next")
			}
			return
		}

		if n.ErrMessage != nil {
			errMsg := *n.ErrMessage
			p.log.Error().Str("error", errMsg).Msg("tailscale.watchStatus: backend")

			if strings.Contains(errMsg, "invalid key") {
				p.log.Error().Msg(
					"the auth key may be invalid, expired, or the tailnet policy requires" +
						" hardware attestation (not supported by tsnet)." +
						" Verify the key is correct and check tailnet policy settings.",
				)
			}

			p.setStatus(model.ProxyStatusError, "", "")
			return
		}

		status, err := p.lc.Status(p.ctx)
		if err != nil {
			if !errors.Is(err, net.ErrClosed) && !errors.Is(err, context.Canceled) {
				p.log.Error().Err(err).Msg("tailscale.watchStatus: status")
				return
			}
			continue
		}

		switch status.BackendState {
		case "NeedsLogin":
			if status.AuthURL != "" {
				p.setStatus(model.ProxyStatusAuthenticating, "", status.AuthURL)
			} else {
				p.log.Info().Msg(
					"tailscale is in NeedsLogin state without an auth URL." +
						" This indicates stale tsnet state (e.g. after power loss, reboot, or changing ephemeral)." +
						" Restart tsdproxy to auto-recover, or manually delete the proxy data directory.",
				)
				p.setStatus(model.ProxyStatusError, "", "")
			}
		case "Starting":
			p.setStatus(model.ProxyStatusStarting, "", "")
		case "Running":
			if status.Self == nil {
				p.log.Warn().Msg("tailscale status Self is nil, skipping")
				continue
			}
			prevStatus := p.getStatus()
			p.setStatus(model.ProxyStatusRunning, strings.TrimRight(status.Self.DNSName, "."), "")
			if prevStatus != model.ProxyStatusRunning && p.hasHTTPSPort() {
				go p.getTLSCertificates()
			}
		}
	}
}

func (p *Proxy) setStatus(status model.ProxyStatus, url string, authURL string) {
	p.mtx.Lock()
	if p.status == status && p.url == url && p.authURL == authURL {
		p.mtx.Unlock()
		return
	}

	p.log.Debug().Str("status", status.String()).Msg("tailscale status")

	p.status = status
	if url != "" {
		p.url = url
	}
	if authURL != "" {
		p.authURL = authURL
	}
	p.mtx.Unlock()

	select {
	case p.events <- model.ProxyEvent{
		Status: status,
	}:
	default:
		p.log.Warn().Msg("dropping proxy event: no listener")
	}
}

func (p *Proxy) getTLSCertificates() {
	p.mtx.Lock()
	lc := p.lc
	tsServer := p.tsServer
	p.mtx.Unlock()

	if lc == nil || tsServer == nil {
		return
	}

	ctx, cancel := context.WithTimeout(p.ctx, 2*time.Minute)
	defer cancel()

	waitStart := time.Now()
	if err := p.certSem.Acquire(ctx, 1); err != nil {
		if !errors.Is(err, context.Canceled) {
			p.log.Error().Err(err).Msg("failed to acquire cert semaphore")
		}
		return
	}
	defer p.certSem.Release(1)

	if wait := time.Since(waitStart); wait > time.Second {
		p.log.Warn().Dur("wait", wait).Msg("cert generation delayed by semaphore contention")
	}

	p.log.Info().Msg("Generating TLS certificate")
	certDomains := tsServer.CertDomains()
	if len(certDomains) == 0 {
		p.log.Warn().Msg("no certificate domains available")
		return
	}

	if _, _, err := lc.CertPair(ctx, certDomains[0]); err != nil {
		if !errors.Is(err, context.Canceled) {
			p.log.Error().Err(err).Msg("error to get TLS certificates")
		}
		return
	}
	p.log.Info().Msg("TLS certificate generated")
}

func (p *Proxy) hasHTTPSPort() bool {
	for _, port := range p.config.Ports {
		if port.ProxyProtocol == "https" {
			return true
		}
	}
	return false
}
