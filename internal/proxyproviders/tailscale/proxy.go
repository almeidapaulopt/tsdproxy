// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package tailscale

import (
	"context"
	"errors"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/almeidapaulopt/tsdproxy/internal/model"
	"github.com/almeidapaulopt/tsdproxy/internal/proxyproviders"

	"github.com/rs/zerolog"
	tsc "tailscale.com/client/tailscale/v2"
	"tailscale.com/ipn"
	"tailscale.com/tsnet"
)

// Proxy struct implements proxyconfig.Proxy.
type Proxy struct {
	log      zerolog.Logger
	config   *model.Config
	tsServer *tsnet.Server
	lc       *tsc.Client
	ctx      context.Context

	events chan model.ProxyEvent

	authURL string
	url     string
	status  model.ProxyStatus

	mtx       sync.Mutex
	closeOnce sync.Once
}

var (
	_ proxyproviders.ProxyInterface = (*Proxy)(nil)

	ErrProxyPortNotFound = errors.New("proxy port not found")
)

// Start method implements proxyconfig.Proxy Start method.
func (p *Proxy) Start(ctx context.Context) error {
	var (
		err error
		lc  *tsc.Client
	)

	if err = p.tsServer.Start(); err != nil {
		return err
	}

	if lc, err = p.tsServer.LocalClient(); err != nil {
		return err
	}

	p.mtx.Lock()
	p.ctx = ctx
	p.lc = lc
	p.mtx.Unlock()

	go p.watchStatus()

	return nil
}

func (p *Proxy) GetURL() string {
	p.mtx.Lock()
	url := p.url
	p.mtx.Unlock()
	// TODO: should be configurable and not force to https
	return "https://" + url
}

func (p *Proxy) getStatus() model.ProxyStatus {
	p.mtx.Lock()
	s := p.status
	p.mtx.Unlock()
	return s
}

// Close method implements proxyconfig.Proxy Close method.
func (p *Proxy) Close() error {
	p.closeOnce.Do(func() {
		close(p.events)
	})

	var err error
	if p.tsServer != nil {
		err = p.tsServer.Close()

		if p.config.Tailscale.Ephemeral && p.tsServer.Dir != "" {
			if removeErr := os.RemoveAll(p.tsServer.Dir); removeErr != nil {
				p.log.Error().Err(removeErr).Msg("failed to clean up ephemeral node state")
			}
		}
	}

	return err
}

func (p *Proxy) GetListener(port string) (net.Listener, error) {
	portCfg, ok := p.config.Ports[port]
	if !ok {
		return nil, ErrProxyPortNotFound
	}

	network := portCfg.ProxyProtocol
	if portCfg.ProxyProtocol == "http" || portCfg.ProxyProtocol == "https" {
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

func (p *Proxy) WatchEvents() chan model.ProxyEvent {
	return p.events
}

func (p *Proxy) GetAuthURL() string {
	p.mtx.Lock()
	authURL := p.authURL
	p.mtx.Unlock()
	return authURL
}

func (p *Proxy) Whois(r *http.Request) model.Whois {
	p.mtx.Lock()
	lc := p.lc
	p.mtx.Unlock()
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

	return model.Whois{
		DisplayName:   who.UserProfile.DisplayName,
		Username:      who.UserProfile.LoginName,
		ID:            who.UserProfile.ID.String(),
		ProfilePicURL: who.UserProfile.ProfilePicURL,
	}
}

func (p *Proxy) watchStatus() {
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
			if !errors.Is(err, net.ErrClosed) {
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
				p.log.Error().Msg(
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
			if prevStatus != model.ProxyStatusRunning {
				p.getTLSCertificates()
			}
		}
	}
}

func (p *Proxy) setStatus(status model.ProxyStatus, url string, authURL string) {
	if p.status == status && p.url == url && p.authURL == authURL {
		return
	}

	p.log.Debug().Str("status", status.String()).Msg("tailscale status")

	p.mtx.Lock()
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

	p.log.Info().Msg("Generating TLS certificate")
	certDomains := tsServer.CertDomains()
	if len(certDomains) == 0 {
		p.log.Warn().Msg("no certificate domains available")
		return
	}
	if _, _, err := lc.CertPair(p.ctx, certDomains[0]); err != nil {
		p.log.Error().Err(err).Msg("error to get TLS certificates")
		return
	}
	p.log.Info().Msg("TLS certificate generated")
}
