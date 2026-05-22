// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package tailscale

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/almeidapaulopt/tsdproxy/internal/model"
	"github.com/rs/zerolog"
	"golang.org/x/sync/semaphore"
	"tailscale.com/client/local"
	"tailscale.com/ipn"
	"tailscale.com/tsnet"
)

type sniPortListener struct {
	router   *SNIRouter
	listener net.Listener
}

// SharedServerConfig holds the configuration for creating a SharedServer.
type SharedServerConfig struct {
	Hostname   string
	DataDir    string
	AuthKey    string
	ControlURL string
	Ephemeral  bool
	CertSem    *semaphore.Weighted
	Log        zerolog.Logger
}

// sharedEventSub wraps a subscriber channel with a done flag.
type sharedEventSub struct {
	ch   chan model.ProxyEvent
	once sync.Once
}

// SharedServer owns a single tsnet.Server shared by multiple proxies.
// It is reference-counted: the tsnet server starts on first Acquire and
// stops when the last proxy releases.
type SharedServer struct {
	tsServer   *tsnet.Server
	lc         *local.Client
	mu         sync.RWMutex
	refCount   int
	started    bool
	url        string
	ctx        context.Context
	cancel     context.CancelFunc
	log        zerolog.Logger
	hostname   string
	datadir    string
	authKey    string
	controlURL string
	ephemeral  bool
	certSem    *semaphore.Weighted
	listeners  map[int]*sniPortListener
	subs       map[*sharedEventSub]struct{}
	watchDone  chan struct{}
	watchOnce  sync.Once
	closeOnce  sync.Once
}

func NewSharedServer(cfg SharedServerConfig) *SharedServer {
	ctx, cancel := context.WithCancel(context.Background())
	return &SharedServer{
		hostname:   cfg.Hostname,
		datadir:    cfg.DataDir,
		authKey:    cfg.AuthKey,
		controlURL: cfg.ControlURL,
		ephemeral:  cfg.Ephemeral,
		certSem:    cfg.CertSem,
		log:        cfg.Log.With().Str("shared_server", cfg.Hostname).Logger(),
		ctx:        ctx,
		cancel:     cancel,
		listeners:  make(map[int]*sniPortListener),
		subs:       make(map[*sharedEventSub]struct{}),
	}
}

func (ss *SharedServer) Acquire(domain string, port int) (*VirtualListener, error) {
	ss.mu.Lock()
	defer ss.mu.Unlock()

	if !ss.started {
		if err := ss.start(); err != nil {
			return nil, fmt.Errorf("shared server start: %w", err)
		}

		ss.watchOnce.Do(func() {
			ss.watchDone = make(chan struct{})
			go ss.watchStatus()
		})
	}

	ss.refCount++

	pl, ok := ss.listeners[port]
	if !ok {
		addr := ":" + strconv.Itoa(port)
		l, err := ss.tsServer.Listen("tcp", addr)
		if err != nil {
			ss.refCount--
			return nil, fmt.Errorf("listen on port %d: %w", port, err)
		}

		router := NewSNIRouter(ss.log.With().Int("port", port).Logger())
		pl = &sniPortListener{
			router:   router,
			listener: l,
		}
		ss.listeners[port] = pl

		go router.Serve(l)
	}

	vl := pl.router.Register(domain)
	ss.log.Info().Str("domain", domain).Int("port", port).Msg("domain registered with shared server")

	return vl, nil
}

func (ss *SharedServer) Release(domain string, port int) {
	ss.mu.Lock()
	defer ss.mu.Unlock()

	if pl, ok := ss.listeners[port]; ok {
		pl.router.Unregister(domain)
	}

	ss.refCount--
	if ss.refCount <= 0 {
		ss.shutdown()
	}
}

func (ss *SharedServer) Start(ctx context.Context) error {
	ss.mu.Lock()
	defer ss.mu.Unlock()

	if ss.started {
		return nil
	}

	if err := ss.start(); err != nil {
		return err
	}

	ss.ctx = ctx

	ss.watchOnce.Do(func() {
		ss.watchDone = make(chan struct{})
		go ss.watchStatus()
	})

	return nil
}

func (ss *SharedServer) start() error {
	controlURL := ss.controlURL
	if controlURL == "" {
		controlURL = model.DefaultTailscaleControlURL
	}

	ss.tsServer = &tsnet.Server{
		Hostname: ss.hostname,
		AuthKey:  ss.authKey,
		Dir:      ss.datadir,
		Ephemeral: ss.ephemeral,
		UserLogf: func(format string, args ...any) {
			ss.log.Info().Msgf(format, args...)
		},
		Logf: func(format string, args ...any) {
			ss.log.Trace().Msgf(format, args...)
		},
		ControlURL: controlURL,
	}

	if err := ss.tsServer.Start(); err != nil {
		return fmt.Errorf("tsnet start: %w", err)
	}

	lc, err := ss.tsServer.LocalClient()
	if err != nil {
		ss.tsServer.Close()
		return fmt.Errorf("local client: %w", err)
	}
	ss.lc = lc
	ss.started = true

	return nil
}

func (ss *SharedServer) watchStatus() {
	defer func() {
		ss.mu.Lock()
		if ss.watchDone != nil {
			close(ss.watchDone)
		}
		ss.mu.Unlock()
	}()

	ss.mu.RLock()
	lc := ss.lc
	ss.mu.RUnlock()
	if lc == nil {
		ss.log.Error().Msg("shared server watchStatus: local client is nil")
		return
	}

	watcher, err := lc.WatchIPNBus(ss.ctx, ipn.NotifyInitialState|ipn.NotifyNoPrivateKeys|ipn.NotifyInitialHealthState)
	if err != nil {
		ss.log.Error().Err(err).Msg("shared server watchStatus")
		return
	}
	defer watcher.Close()

	for {
		n, err := watcher.Next()
		if err != nil {
			if !errors.Is(err, context.Canceled) {
				ss.log.Error().Err(err).Msg("shared server watchStatus: Next")
			}
			return
		}

		if n.ErrMessage != nil {
			errMsg := *n.ErrMessage
			ss.log.Error().Str("error", errMsg).Msg("shared server watchStatus: backend")
			ss.sendEvent(model.ProxyEvent{Status: model.ProxyStatusError})
			return
		}

		status, err := lc.Status(ss.ctx)
		if err != nil {
			if !errors.Is(err, net.ErrClosed) && !errors.Is(err, context.Canceled) {
				ss.log.Error().Err(err).Msg("shared server watchStatus: status")
				return
			}
			continue
		}

		switch status.BackendState {
		case "NeedsLogin":
			ss.log.Info().Msg("shared server in NeedsLogin state")
			ss.sendEvent(model.ProxyEvent{Status: model.ProxyStatusAuthenticating})
		case "Starting":
			ss.sendEvent(model.ProxyEvent{Status: model.ProxyStatusStarting})
		case "Running":
			if status.Self == nil {
				ss.log.Warn().Msg("shared server status Self is nil, skipping")
				continue
			}
			dnsName := strings.TrimRight(status.Self.DNSName, ".")
			ss.mu.Lock()
			ss.url = dnsName
			ss.mu.Unlock()
			ss.sendEvent(model.ProxyEvent{Status: model.ProxyStatusRunning})
			go ss.getTLSCertificates()
		}
	}
}

func (ss *SharedServer) getTLSCertificates() {
	ss.mu.RLock()
	lc := ss.lc
	tsServer := ss.tsServer
	certSem := ss.certSem
	ss.mu.RUnlock()

	if lc == nil || tsServer == nil || certSem == nil {
		return
	}

	ctx, cancel := context.WithTimeout(ss.ctx, 2*time.Minute)
	defer cancel()

	if err := certSem.Acquire(ctx, 1); err != nil {
		if !errors.Is(err, context.Canceled) {
			ss.log.Error().Err(err).Msg("failed to acquire cert semaphore")
		}
		return
	}
	defer certSem.Release(1)

	ss.log.Info().Msg("Generating TLS certificate for shared server")
	certDomains := tsServer.CertDomains()
	if len(certDomains) == 0 {
		ss.log.Warn().Msg("no certificate domains available")
		return
	}

	if _, _, err := lc.CertPair(ctx, certDomains[0]); err != nil {
		if !errors.Is(err, context.Canceled) {
			ss.log.Error().Err(err).Msg("error getting TLS certificates for shared server")
		}
		return
	}
	ss.log.Info().Msg("TLS certificate generated for shared server")
}

func (ss *SharedServer) sendEvent(status model.ProxyEvent) {
	ss.mu.RLock()
	defer ss.mu.RUnlock()

	for sub := range ss.subs {
		select {
		case sub.ch <- status:
		default:
			ss.log.Warn().Msg("dropping shared server event: subscriber channel full")
		}
	}
}

// SubscribeEvents returns a channel that receives status events from the
// shared server. Call UnsubscribeEvents to clean up.
func (ss *SharedServer) SubscribeEvents() chan model.ProxyEvent {
	sub := &sharedEventSub{
		ch: make(chan model.ProxyEvent, 16), //nolint:mnd
	}
	ss.mu.Lock()
	ss.subs[sub] = struct{}{}
	ss.mu.Unlock()
	return sub.ch
}

// UnsubscribeEvents removes an event subscription.
func (ss *SharedServer) UnsubscribeEvents(ch chan model.ProxyEvent) {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	for sub := range ss.subs {
		if sub.ch == ch {
			sub.once.Do(func() { close(sub.ch) })
			delete(ss.subs, sub)
			return
		}
	}
}

func (ss *SharedServer) shutdown() {
	for _, pl := range ss.listeners {
		pl.router.CloseAll()
		pl.listener.Close()
	}
	ss.listeners = make(map[int]*sniPortListener)

	if ss.tsServer != nil {
		ss.tsServer.Close()
	}
	ss.cancel()
	ss.started = false

	ss.closeOnce.Do(func() {
		for sub := range ss.subs {
			sub.once.Do(func() { close(sub.ch) })
		}
		ss.subs = make(map[*sharedEventSub]struct{})
	})
}

func (ss *SharedServer) GetURL() string {
	ss.mu.RLock()
	defer ss.mu.RUnlock()
	return ss.url
}

func (ss *SharedServer) GetLocalClient() *local.Client {
	ss.mu.RLock()
	defer ss.mu.RUnlock()
	return ss.lc
}

func (ss *SharedServer) Whois(r *http.Request) model.Whois {
	ss.mu.RLock()
	lc := ss.lc
	ss.mu.RUnlock()
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

func (ss *SharedServer) Close() {
	ss.mu.Lock()

	if !ss.started {
		ss.closeOnce.Do(func() {
			for sub := range ss.subs {
				sub.once.Do(func() { close(sub.ch) })
			}
			ss.subs = make(map[*sharedEventSub]struct{})
		})
		ss.mu.Unlock()
		return
	}

	watchDone := ss.watchDone
	ss.mu.Unlock()

	ss.cancel()

	if watchDone != nil {
		<-watchDone
	}

	ss.mu.Lock()
	ss.shutdown()
	ss.mu.Unlock()
}
