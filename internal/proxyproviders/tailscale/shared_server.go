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
	"sync/atomic"
	"time"

	"github.com/rs/zerolog"
	"golang.org/x/sync/semaphore"
	"tailscale.com/client/local"
	"tailscale.com/ipn"
	"tailscale.com/tsnet"

	"github.com/almeidapaulopt/tsdproxy/internal/model"
)

type portEntry struct {
	listener net.Listener
	router   *PortRouter
	owner    string
}

// SharedServerConfig holds the configuration for creating a SharedServer.
type SharedServerConfig struct {
	Log        zerolog.Logger
	CertSem    *semaphore.Weighted
	Hostname   string
	DataDir    string
	AuthKey    string
	ControlURL string
	Ephemeral  bool
}

// sharedEventSub wraps a subscriber channel with a done flag.
type sharedEventSub struct {
	ch   chan model.ProxyEvent
	once sync.Once
}

// sharedState represents the state of the SharedServer state machine.
type sharedState int

const (
	sharedIdle sharedState = iota
	sharedRunning
	sharedClosed
)

// sharedRuntime holds all mutable state owned exclusively by the loop goroutine.
type sharedRuntime struct {
	ctx          context.Context
	cancel       context.CancelFunc
	tsServer     *tsnet.Server
	lc           *local.Client
	listeners    map[int]*portEntry
	packetRoutes map[int]net.PacketConn // port → PacketConn for UDP
	subs         map[*sharedEventSub]struct{}
	watchDone    chan struct{}
	url          string
	gen          int
	routeCount   int
	certInFlight bool
}

// Command types for the state machine.

type sharedCmd interface{ cmd() }

type acquireCmd struct {
	reply    chan acquireResult
	domain   string
	port     int
	protocol string
}

func (acquireCmd) cmd() {}

type acquireResult struct {
	vl     *VirtualListener
	direct net.Listener
	err    error
}

type acquirePacketCmd struct {
	reply  chan acquirePacketResult
	domain string
	port   int
}

func (acquirePacketCmd) cmd() {}

type acquirePacketResult struct {
	pc  net.PacketConn
	err error
}

type releaseCmd struct {
	reply    chan struct{}
	domain   string
	port     int
	protocol string
}

func (releaseCmd) cmd() {}

type releasePacketCmd struct {
	reply  chan struct{}
	domain string
	port   int
}

func (releasePacketCmd) cmd() {}

type closeCmd struct {
	reply chan error
}

func (closeCmd) cmd() {}

type getURLCmd struct {
	reply chan string
}

func (getURLCmd) cmd() {}

type getLocalClientCmd struct {
	reply chan *local.Client
}

func (getLocalClientCmd) cmd() {}

type subscribeCmd struct {
	reply chan chan model.ProxyEvent
}

func (subscribeCmd) cmd() {}

type unsubscribeCmd struct {
	ch    chan model.ProxyEvent
	reply chan struct{}
}

func (unsubscribeCmd) cmd() {}

type watchUpdateCmd struct {
	url string
	evt model.ProxyEvent
	gen int
}

func (watchUpdateCmd) cmd() {}

type certDoneCmd struct {
	gen int
}

func (certDoneCmd) cmd() {}

type idleTimeoutCmd struct{}

func (idleTimeoutCmd) cmd() {}

// sharedIdleTimeout is how long the shared server stays running after the last
// proxy is released. Prevents tsnet teardown/restart flapping during rolling
// restarts or when containers cycle quickly.
const sharedIdleTimeout = 30 * time.Second
// All mutable state is managed by a single event-loop goroutine.
// Public methods are thin wrappers that send commands and wait for replies.
type SharedServer struct {
	log        zerolog.Logger
	certSem    *semaphore.Weighted
	cmds       chan sharedCmd
	done       chan struct{}
	hostname   string
	datadir    string
	authKey    string
	controlURL string
	closed     atomic.Bool
	ephemeral  bool
}

// NewSharedServer creates a SharedServer and starts the event loop.
func NewSharedServer(cfg SharedServerConfig) *SharedServer {
	ss := &SharedServer{
		hostname:   cfg.Hostname,
		datadir:    cfg.DataDir,
		authKey:    cfg.AuthKey,
		controlURL: cfg.ControlURL,
		ephemeral:  cfg.Ephemeral,
		certSem:    cfg.CertSem,
		log:        cfg.Log.With().Str("shared_server", cfg.Hostname).Logger(),
		cmds:       make(chan sharedCmd, 64), //nolint:mnd
		done:       make(chan struct{}),
	}
	go ss.loop()
	return ss
}

// sendProducer sends a command from a producer goroutine (watchStatus, getCertificates).
// It aborts if the loop has exited or the producer's context was canceled,
// preventing deadlock when the loop is blocked in stopRuntime.
func (ss *SharedServer) sendProducer(ctx context.Context, cmd sharedCmd) bool {
	if ctx == nil {
		select {
		case ss.cmds <- cmd:
			return true
		case <-ss.done:
			return false
		}
	}
	select {
	case ss.cmds <- cmd:
		return true
	case <-ss.done:
		return false
	case <-ctx.Done():
		return false
	}
}

// sendPublic sends a command from a public method. Returns false if the
// server is closed (loop has exited), preventing goroutine leaks.
func (ss *SharedServer) sendPublic(cmd sharedCmd) bool {
	if ss.closed.Load() {
		return false
	}
	select {
	case ss.cmds <- cmd:
		return true
	case <-ss.done:
		return false
	}
}

// loop is the core event loop. It owns all mutable state.
func (ss *SharedServer) loop() {
	defer close(ss.done)

	state := sharedIdle
	var rt *sharedRuntime // nil when idle
	var nextGen int       // monotonic generation counter, loop-scoped
	var idleTimer *time.Timer

	for cmd := range ss.cmds {
		// Cancel idle timer only for state-mutating commands.
		// Read-only commands (watchUpdate, certDone, getURL, getLocalClient,
		// subscribe, unsubscribe) must not disrupt the idle countdown.
		switch cmd.(type) {
		case acquireCmd, acquirePacketCmd, releaseCmd, releasePacketCmd,
			closeCmd, idleTimeoutCmd:
			if idleTimer != nil {
				idleTimer.Stop()
				idleTimer = nil
			}
		}

		switch c := cmd.(type) {
		case acquireCmd:
			state, rt = ss.handleAcquire(c, state, rt, &nextGen)
		case releaseCmd:
			state, rt = ss.handleRelease(c, state, rt)
			if state == sharedIdle && rt != nil {
				idleTimer = time.AfterFunc(sharedIdleTimeout, func() {
					select {
					case ss.cmds <- idleTimeoutCmd{}:
					case <-ss.done:
					}
				})
			}
		case acquirePacketCmd:
			state, rt = ss.handleAcquirePacket(c, state, rt, &nextGen)
		case releasePacketCmd:
			state, rt = ss.handleReleasePacket(c, state, rt)
			if state == sharedIdle && rt != nil {
				idleTimer = time.AfterFunc(sharedIdleTimeout, func() {
					select {
					case ss.cmds <- idleTimeoutCmd{}:
					case <-ss.done:
					}
				})
			}
		case idleTimeoutCmd:
			// Only stop if we're still idle (acquire after release cancels the timer).
			if state == sharedIdle && rt != nil {
				ss.log.Info().Msg("shared server idle timeout, stopping")
				ss.stopRuntime(rt)
				rt = nil
			}
		case closeCmd:
			ss.handleClose(c, state, rt)
			return
		case watchUpdateCmd:
			ss.handleWatchUpdate(c, rt)
		case certDoneCmd:
			ss.handleCertDone(c, rt)
		case getURLCmd:
			ss.handleGetURL(c, rt)
		case getLocalClientCmd:
			ss.handleGetLocalClient(c, rt)
		case subscribeCmd:
			rt = ss.handleSubscribe(c, rt)
		case unsubscribeCmd:
			ss.handleUnsubscribe(c, rt)
		}
	}
}

func (ss *SharedServer) handleAcquire(c acquireCmd, state sharedState, rt *sharedRuntime, nextGen *int) (sharedState, *sharedRuntime) {
	switch state {
	case sharedIdle:
		if rt == nil {
			newRT := ss.startRuntime(nil, *nextGen)
			if newRT == nil {
				c.reply <- acquireResult{nil, nil, errors.New("shared server start failed")}
				return state, rt
			}
			(*nextGen)++
			rt = newRT
		}
		state = sharedRunning
		vl, direct, err := ss.registerRoute(rt, c.domain, c.port, c.protocol)
		if err != nil {
			if rt.routeCount <= 0 {
				ss.stopRuntime(rt)
				rt = nil
				state = sharedIdle
			}
			c.reply <- acquireResult{nil, nil, err}
			return state, rt
		}
		c.reply <- acquireResult{vl, direct, nil}

	case sharedRunning:
		vl, direct, err := ss.registerRoute(rt, c.domain, c.port, c.protocol)
		if err != nil {
			if rt.routeCount <= 0 {
				ss.stopRuntime(rt)
				rt = nil
				state = sharedIdle
			}
			c.reply <- acquireResult{nil, nil, err}
			return state, rt
		}
		c.reply <- acquireResult{vl, direct, nil}

	case sharedClosed:
		c.reply <- acquireResult{nil, nil, errors.New("shared server closed")}
	}
	return state, rt
}

func (ss *SharedServer) handleRelease(c releaseCmd, state sharedState, rt *sharedRuntime) (sharedState, *sharedRuntime) {
	if state == sharedRunning && rt != nil {
		ss.unregisterRoute(rt, c.domain, c.port, c.protocol)
		if rt.routeCount <= 0 {
			c.reply <- struct{}{}
			return sharedIdle, rt
		}
	}
	c.reply <- struct{}{}
	return state, rt
}

func (ss *SharedServer) handleClose(c closeCmd, state sharedState, rt *sharedRuntime) {
	if rt != nil {
		ss.stopRuntime(rt)
	}
	ss.closed.Store(true)
	c.reply <- nil
}

func (ss *SharedServer) handleAcquirePacket(c acquirePacketCmd, state sharedState, rt *sharedRuntime, nextGen *int) (sharedState, *sharedRuntime) {
	switch state {
	case sharedIdle:
		if rt == nil {
			newRT := ss.startRuntime(nil, *nextGen)
			if newRT == nil {
				c.reply <- acquirePacketResult{nil, errors.New("shared server start failed")}
				return state, rt
			}
			(*nextGen)++
			rt = newRT
		}
		state = sharedRunning
		fallthrough
	case sharedRunning:
		pc, err := ss.registerPacketRoute(rt, c.domain, c.port)
		if err != nil {
			if rt.routeCount <= 0 {
				ss.stopRuntime(rt)
				rt = nil
				state = sharedIdle
			}
			c.reply <- acquirePacketResult{nil, err}
			return state, rt
		}
		c.reply <- acquirePacketResult{pc, nil}
	case sharedClosed:
		c.reply <- acquirePacketResult{nil, errors.New("shared server closed")}
	}
	return state, rt
}

func (ss *SharedServer) handleReleasePacket(c releasePacketCmd, state sharedState, rt *sharedRuntime) (sharedState, *sharedRuntime) {
	if rt != nil {
		ss.unregisterPacketRoute(rt, c.domain, c.port)
		if rt.routeCount <= 0 {
			c.reply <- struct{}{}
			return sharedIdle, rt
		}
	}
	c.reply <- struct{}{}
	return state, rt
}

func (ss *SharedServer) handleWatchUpdate(c watchUpdateCmd, rt *sharedRuntime) {
	if rt == nil || c.gen != rt.gen {
		return
	}
	if c.url != "" {
		rt.url = c.url
	}
	for sub := range rt.subs {
		select {
		case sub.ch <- c.evt:
		default:
			ss.log.Warn().Msg("dropping shared server event: subscriber channel full")
		}
	}
	if c.evt.Status == model.ProxyStatusRunning && !rt.certInFlight {
		rt.certInFlight = true
		go ss.getCertificates(rt.ctx, rt.gen, rt.lc, rt.tsServer)
	}
}

func (ss *SharedServer) handleCertDone(c certDoneCmd, rt *sharedRuntime) {
	if rt != nil && c.gen == rt.gen {
		rt.certInFlight = false
	}
}

func (ss *SharedServer) handleGetURL(c getURLCmd, rt *sharedRuntime) {
	url := ""
	if rt != nil {
		url = rt.url
	}
	c.reply <- url
}

func (ss *SharedServer) handleGetLocalClient(c getLocalClientCmd, rt *sharedRuntime) {
	var lc *local.Client
	if rt != nil {
		lc = rt.lc
	}
	c.reply <- lc
}

func (ss *SharedServer) handleSubscribe(c subscribeCmd, rt *sharedRuntime) *sharedRuntime {
	if rt == nil {
		rt = &sharedRuntime{
			subs: make(map[*sharedEventSub]struct{}),
		}
	}
	sub := &sharedEventSub{
		ch: make(chan model.ProxyEvent, 16), //nolint:mnd
	}
	rt.subs[sub] = struct{}{}
	c.reply <- sub.ch
	return rt
}

func (ss *SharedServer) handleUnsubscribe(c unsubscribeCmd, rt *sharedRuntime) {
	if rt != nil {
		for sub := range rt.subs {
			if sub.ch == c.ch {
				sub.once.Do(func() { close(sub.ch) })
				delete(rt.subs, sub)
				break
			}
		}
	}
	c.reply <- struct{}{}
}

// startRuntime creates a new tsnet.Server, starts it, and begins watching status.
// If an existing subscriber-only runtime is passed, its subscribers are transferred.
func (ss *SharedServer) startRuntime(prevRT *sharedRuntime, gen int) *sharedRuntime {
	controlURL := ss.controlURL
	if controlURL == "" {
		controlURL = model.DefaultTailscaleControlURL
	}

	ctx, cancel := context.WithCancel(context.Background())

	tsServer := &tsnet.Server{
		Hostname:   ss.hostname,
		AuthKey:    ss.authKey,
		Dir:        ss.datadir,
		Ephemeral:  ss.ephemeral,
		ControlURL: controlURL,
		UserLogf:   func(format string, args ...any) { ss.log.Info().Msgf(format, args...) },
		Logf:       func(format string, args ...any) { ss.log.Trace().Msgf(format, args...) },
	}

	if err := tsServer.Start(); err != nil {
		ss.log.Error().Err(err).Msg("failed to start tsnet server")
		cancel()
		return nil
	}

	lc, err := tsServer.LocalClient()
	if err != nil {
		ss.log.Error().Err(err).Msg("failed to get local client")
		tsServer.Close()
		cancel()
		return nil
	}

	// Transfer subscribers from previous runtime if any.
	subs := make(map[*sharedEventSub]struct{})
	if prevRT != nil {
		for sub := range prevRT.subs {
			subs[sub] = struct{}{}
		}
	}

	rt := &sharedRuntime{
		gen:          gen,
		ctx:          ctx,
		cancel:       cancel,
		tsServer:     tsServer,
		lc:           lc,
		listeners:    make(map[int]*portEntry),
		packetRoutes: make(map[int]net.PacketConn),
		subs:         subs,
	}

	// Start watcher.
	watchDone := make(chan struct{})
	rt.watchDone = watchDone
	go ss.watchStatus(ctx, rt.gen, lc, watchDone)

	return rt
}

// registerRoute registers a domain on the given port, creating the port listener if needed.
func (ss *SharedServer) registerRoute(rt *sharedRuntime, domain string, port int, protocol string) (*VirtualListener, net.Listener, error) {
	entry, exists := rt.listeners[port]

	switch protocol {
	case model.ProtoHTTPS, model.ProtoHTTP:
		if !exists {
			if _, udpExists := rt.packetRoutes[port]; udpExists {
				return nil, nil, fmt.Errorf("port %d already in use as UDP port", port)
			}

			addr := ":" + strconv.Itoa(port)
			l, err := rt.tsServer.Listen("tcp", addr)
			if err != nil {
				return nil, nil, fmt.Errorf("listen on port %d: %w", port, err)
			}

			mode := RouteSNI
			if protocol == model.ProtoHTTP {
				mode = RouteHTTPHost
			}

			router := NewPortRouter(mode, ss.log.With().Int("port", port).Logger())
			entry = &portEntry{
				listener: l,
				router:   router,
			}
			rt.listeners[port] = entry
			go router.Serve(l)
		}

		if entry.router == nil {
			return nil, nil, fmt.Errorf("port %d is already in use as a direct (TCP/UDP) port", port)
		}

		expectedMode := RouteSNI
		if protocol == model.ProtoHTTP {
			expectedMode = RouteHTTPHost
		}
		if entry.router.mode != expectedMode {
			return nil, nil, fmt.Errorf("port %d routing mode conflict: already %v, requested %v", port, entry.router.mode, expectedMode)
		}

		vl, err := entry.router.Register(domain)
		if err != nil {
			return nil, nil, err
		}
		rt.routeCount++
		ss.log.Info().Str("domain", domain).Int("port", port).Str("protocol", protocol).Msg("domain registered with shared server")
		return vl, nil, nil

	case model.ProtoTCP:
		if exists {
			return nil, nil, fmt.Errorf("port %d already claimed by %q", port, entry.owner)
		}
		if _, udpExists := rt.packetRoutes[port]; udpExists {
			return nil, nil, fmt.Errorf("port %d already in use as UDP port", port)
		}

		addr := ":" + strconv.Itoa(port)
		l, err := rt.tsServer.Listen("tcp", addr)
		if err != nil {
			return nil, nil, fmt.Errorf("listen on port %d: %w", port, err)
		}

		rt.listeners[port] = &portEntry{
			listener: l,
			owner:    domain,
		}
		rt.routeCount++
		ss.log.Info().Str("domain", domain).Int("port", port).Str("protocol", protocol).Msg("TCP port registered with shared server")
		return nil, l, nil

	default:
		return nil, nil, fmt.Errorf("unsupported protocol %q for shared proxy TCP routing", protocol)
	}
}

func (ss *SharedServer) registerPacketRoute(rt *sharedRuntime, domain string, port int) (net.PacketConn, error) {
	if _, exists := rt.listeners[port]; exists {
		return nil, fmt.Errorf("port %d already in use", port)
	}
	if _, exists := rt.packetRoutes[port]; exists {
		return nil, fmt.Errorf("UDP port %d already claimed", port)
	}

	addr := ":" + strconv.Itoa(port)
	pc, err := rt.tsServer.ListenPacket("udp", addr)
	if err != nil {
		return nil, fmt.Errorf("listen packet on port %d: %w", port, err)
	}
	rt.packetRoutes[port] = pc
	rt.routeCount++
	ss.log.Info().Str("domain", domain).Int("port", port).Str("protocol", "udp").Msg("UDP port registered with shared server")
	return pc, nil
}

// unregisterRoute removes a domain registration from the given port.
func (ss *SharedServer) unregisterRoute(rt *sharedRuntime, domain string, port int, protocol string) {
	entry, ok := rt.listeners[port]
	if !ok {
		return
	}

	switch protocol {
	case model.ProtoHTTPS, model.ProtoHTTP:
		if entry.router != nil && entry.router.Unregister(domain) {
			rt.routeCount--
			if entry.router.IsEmpty() {
				entry.router.CloseAll()
				entry.listener.Close()
				delete(rt.listeners, port)
			}
		}
	case model.ProtoTCP:
		entry.listener.Close()
		delete(rt.listeners, port)
		rt.routeCount--
	}
}

func (ss *SharedServer) unregisterPacketRoute(rt *sharedRuntime, domain string, port int) {
	if pc, ok := rt.packetRoutes[port]; ok {
		pc.Close()
		delete(rt.packetRoutes, port)
		rt.routeCount--
		ss.log.Info().Str("domain", domain).Int("port", port).Msg("UDP port unregistered from shared server")
	}
}

// stopRuntime tears down the tsnet server, all listeners, and notifies subscribers.
func (ss *SharedServer) stopRuntime(rt *sharedRuntime) {
	for _, pc := range rt.packetRoutes {
		pc.Close()
	}

	for _, entry := range rt.listeners {
		if entry.router != nil {
			entry.router.CloseAll()
		}
		if entry.listener != nil {
			entry.listener.Close()
		}
	}

	// Close tsnet server.
	if rt.tsServer != nil {
		rt.tsServer.Close()
	}

	// Cancel context (signals watchStatus to stop).
	if rt.cancel != nil {
		rt.cancel()
	}

	// Wait for watcher to finish.
	if rt.watchDone != nil {
		<-rt.watchDone
	}

	// Close all subscriber channels.
	for sub := range rt.subs {
		sub.once.Do(func() { close(sub.ch) })
	}
}

// watchStatus is a pure event producer. It sends watchUpdateCmd events to the loop.
func (ss *SharedServer) watchStatus(ctx context.Context, gen int, lc *local.Client, done chan struct{}) {
	defer close(done)

	if lc == nil {
		ss.log.Error().Msg("shared server watchStatus: local client is nil")
		return
	}

	watcher, err := lc.WatchIPNBus(ctx, ipn.NotifyInitialState|ipn.NotifyNoPrivateKeys|ipn.NotifyInitialHealthState)
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

			if strings.Contains(errMsg, "invalid key") {
				ss.log.Error().Msg(
					"the auth key may be invalid, expired, or the tailnet policy requires" +
						" hardware attestation (not supported by tsnet)." +
						" Verify the key is correct and check tailnet policy settings.",
				)
			}

			ss.sendProducer(ctx, watchUpdateCmd{gen: gen, evt: model.ProxyEvent{Status: model.ProxyStatusError}})
			return
		}

		status, err := lc.Status(ctx)
		if err != nil {
			if !errors.Is(err, net.ErrClosed) && !errors.Is(err, context.Canceled) {
				ss.log.Error().Err(err).Msg("shared server watchStatus: status")
				return
			}
			continue
		}

		switch status.BackendState {
		case "NeedsLogin":
			if status.AuthURL != "" {
				ss.log.Info().Msg("shared server in NeedsLogin state, waiting for interactive auth")
			} else {
				ss.log.Info().Msg(
					"shared server in NeedsLogin state without an auth URL." +
						" This indicates stale tsnet state (e.g. after power loss, reboot, or changing ephemeral)." +
						" Restart tsdproxy to auto-recover, or manually delete the shared server data directory.",
				)
			}
			ss.sendProducer(ctx, watchUpdateCmd{gen: gen, evt: model.ProxyEvent{Status: model.ProxyStatusAuthenticating}})
		case "Starting":
			ss.sendProducer(ctx, watchUpdateCmd{gen: gen, evt: model.ProxyEvent{Status: model.ProxyStatusStarting}})
		case "Running":
			if status.Self == nil {
				ss.log.Warn().Msg("shared server status Self is nil, skipping")
				continue
			}
			dnsName := strings.TrimRight(status.Self.DNSName, ".")
			ss.sendProducer(ctx, watchUpdateCmd{gen: gen, evt: model.ProxyEvent{Status: model.ProxyStatusRunning}, url: dnsName})
		}
	}
}

// getCertificates is a pure event producer. It sends certDoneCmd when finished.
func (ss *SharedServer) getCertificates(ctx context.Context, gen int, lc *local.Client, tsServer *tsnet.Server) {
	defer func() {
		ss.sendProducer(ctx, certDoneCmd{gen: gen})
	}()

	acquireCert(ctx, lc, tsServer, ss.certSem, ss.log)
}

// Public methods — thin wrappers that send a command and wait for the reply.

// Acquire registers a domain on the given port, starting the tsnet server if needed.
func (ss *SharedServer) Acquire(domain string, port int, protocol string) (*VirtualListener, net.Listener, error) {
	cmd := acquireCmd{domain: domain, port: port, protocol: protocol, reply: make(chan acquireResult, 1)}
	if !ss.sendPublic(cmd) {
		return nil, nil, errors.New("shared server closed")
	}
	select {
	case result := <-cmd.reply:
		return result.vl, result.direct, result.err
	case <-ss.done:
		return nil, nil, errors.New("shared server closed")
	}
}

func (ss *SharedServer) Release(domain string, port int, protocol string) {
	cmd := releaseCmd{domain: domain, port: port, protocol: protocol, reply: make(chan struct{}, 1)}
	if !ss.sendPublic(cmd) {
		return
	}
	select {
	case <-cmd.reply:
	case <-ss.done:
	}
}

func (ss *SharedServer) AcquirePacket(domain string, port int) (net.PacketConn, error) {
	cmd := acquirePacketCmd{domain: domain, port: port, reply: make(chan acquirePacketResult, 1)}
	if !ss.sendPublic(cmd) {
		return nil, errors.New("shared server closed")
	}
	select {
	case result := <-cmd.reply:
		return result.pc, result.err
	case <-ss.done:
		return nil, errors.New("shared server closed")
	}
}

func (ss *SharedServer) ReleasePacket(domain string, port int) {
	cmd := releasePacketCmd{domain: domain, port: port, reply: make(chan struct{}, 1)}
	if !ss.sendPublic(cmd) {
		return
	}
	select {
	case <-cmd.reply:
	case <-ss.done:
	}
}

// Close shuts down the shared server permanently.
func (ss *SharedServer) Close() {
	if ss.closed.Load() {
		return
	}
	cmd := closeCmd{reply: make(chan error, 1)}
	if !ss.sendPublic(cmd) {
		return
	}
	select {
	case <-cmd.reply:
	case <-ss.done:
	}
}

// GetURL returns the current Tailscale DNS name, or empty string if not running.
func (ss *SharedServer) GetURL() string {
	cmd := getURLCmd{reply: make(chan string, 1)}
	if !ss.sendPublic(cmd) {
		return ""
	}
	select {
	case v := <-cmd.reply:
		return v
	case <-ss.done:
		return ""
	}
}

// GetLocalClient returns the Tailscale local client, or nil if not running.
func (ss *SharedServer) GetLocalClient() *local.Client {
	cmd := getLocalClientCmd{reply: make(chan *local.Client, 1)}
	if !ss.sendPublic(cmd) {
		return nil
	}
	select {
	case lc := <-cmd.reply:
		return lc
	case <-ss.done:
		return nil
	}
}

// SubscribeEvents returns a channel that receives status events from the
// shared server. Call UnsubscribeEvents to clean up.
func (ss *SharedServer) SubscribeEvents() chan model.ProxyEvent {
	cmd := subscribeCmd{reply: make(chan chan model.ProxyEvent, 1)}
	if !ss.sendPublic(cmd) {
		return nil
	}
	select {
	case ch := <-cmd.reply:
		return ch
	case <-ss.done:
		return nil
	}
}

// UnsubscribeEvents removes an event subscription.
func (ss *SharedServer) UnsubscribeEvents(ch chan model.ProxyEvent) {
	cmd := unsubscribeCmd{ch: ch, reply: make(chan struct{}, 1)}
	if !ss.sendPublic(cmd) {
		return
	}
	select {
	case <-cmd.reply:
	case <-ss.done:
	}
}

// Whois returns identity information for the request's remote address.
func (ss *SharedServer) Whois(r *http.Request) model.Whois {
	return whoisFromLocalClient(ss.GetLocalClient(), r)
}
