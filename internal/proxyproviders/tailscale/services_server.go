// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package tailscale

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog"
	"golang.org/x/sync/semaphore"
	"tailscale.com/client/local"
	"tailscale.com/client/tailscale/v2"
	"tailscale.com/tsnet"

	"github.com/almeidapaulopt/tsdproxy/internal/model"
)

// servicesIdleTimeout is how long the services server stays running after the
// last service is released.
const servicesIdleTimeout = 30 * time.Second

// vipServiceAPI abstracts VIP service API calls for testability.
type vipServiceAPI interface {
	createOrUpdateVIPService(serviceName string, ports []string) error
	deleteVIPService(serviceName string) error
}

// serviceListenerFactory abstracts tsnet.Server.ListenService for testability.
// In production, tsnetServerFactory wraps *tsnet.Server.
type serviceListenerFactory interface {
	ListenService(name string, mode tsnet.ServiceMode) (*tsnet.ServiceListener, error)
	Close(*tsnet.ServiceListener) error
}

// tsnetServerFactory adapts *tsnet.Server to satisfy serviceListenerFactory.
type tsnetServerFactory struct{ server *tsnet.Server }

func (f tsnetServerFactory) ListenService(name string, mode tsnet.ServiceMode) (*tsnet.ServiceListener, error) {
	return f.server.ListenService(name, mode)
}

func (f tsnetServerFactory) Close(sl *tsnet.ServiceListener) error {
	return sl.Close()
}

// ServicesServerConfig holds the configuration for creating a ServicesServer.
type ServicesServerConfig struct {
	Log                 zerolog.Logger
	VIPServiceAPI       vipServiceAPI
	APIClient           *tailscale.Client
	APIFactory          *APIClientFactory
	AuthManager         *AuthManager
	DeviceReconciler    *DeviceReconciler
	LifecycleConfig     *NodeLifecycleConfig
	LifecycleProvider   NodeLifecycleProvider
	CertSem             *semaphore.Weighted
	Hostname            string
	DataDir             string
	AuthKey             string
	ControlURL          string
	Tags                string
	Ephemeral           bool
	AutoApproveDevices  bool
	AutoRemoveConflicts bool
}

// serviceEntry tracks a single service listener.
type serviceEntry struct {
	listener    *tsnet.ServiceListener
	serviceName string
	port        uint16
	https       bool
}

// servicesState represents the state of the ServicesServer state machine.
type servicesState int

const (
	servicesIdle servicesState = iota
	servicesRunning
	servicesClosed // reserved: will be used when event forwarding is added
)

// servicesRuntime holds all mutable state owned exclusively by the loop goroutine.
type servicesRuntime struct {
	ctx          context.Context
	factory      serviceListenerFactory
	cancel       context.CancelFunc
	tsServer     *tsnet.Server
	listeners    map[string]*serviceEntry
	lifecycle    *NodeLifecycle
	bridgeDone   chan struct{}
	authURL      string
	refCount     int
	pendingCount int
	gen          int
	running      bool
}

// Command types for the state machine.

type servicesCmd interface{ cmd() }

type acquireServiceCmd struct {
	reply       chan acquireServiceResult
	serviceName string
	port        uint16
	https       bool
	tcp         bool
}

func (acquireServiceCmd) cmd() {}

type acquireServiceResult struct {
	listener *tsnet.ServiceListener
	err      error
}

type releaseServiceCmd struct {
	reply       chan error
	serviceName string
	port        uint16
}

func (releaseServiceCmd) cmd() {}

type servicesCloseCmd struct {
	reply chan error
}

func (servicesCloseCmd) cmd() {}

type servicesIdleTimeoutCmd struct {
	gen int
}

func (servicesIdleTimeoutCmd) cmd() {}

type servicesWatchUpdateCmd struct {
	authURL string
	status  model.ProxyStatus
}

func (servicesWatchUpdateCmd) cmd() {}

type servicesGetAuthURLCmd struct {
	reply chan string
}

func (servicesGetAuthURLCmd) cmd() {}

type acquireServiceWorkResultCmd struct {
	err      error
	listener *tsnet.ServiceListener
	original acquireServiceCmd
	gen      int
}

func (acquireServiceWorkResultCmd) cmd() {}

// ServicesServer manages a shared, ref-counted tsnet.Server for services mode.
// All mutable state is managed by a single event-loop goroutine.
type ServicesServer struct {
	log                 zerolog.Logger
	vipAPI              vipServiceAPI
	localClient         atomic.Pointer[local.Client]
	apiClient           *tailscale.Client
	apiFactory          *APIClientFactory
	authManager         *AuthManager
	deviceReconciler    *DeviceReconciler
	lifecycleCfg        *NodeLifecycleConfig
	lifecycleProvider   NodeLifecycleProvider
	certSem             *semaphore.Weighted
	ev                  *EventLoop[servicesCmd]
	whoisCache          *WhoisCache
	hostname            string
	datadir             string
	controlURL          string
	tags                string
	authKey             string
	authURL             string // fallback for pre-runtime authURL events
	ephemeral           bool
	autoApproveDevices  bool
	autoRemoveConflicts bool
}

// NewServicesServer creates a ServicesServer and starts the event loop.
func NewServicesServer(cfg ServicesServerConfig) *ServicesServer {
	ss := &ServicesServer{
		hostname:            cfg.Hostname,
		datadir:             cfg.DataDir,
		authKey:             cfg.AuthKey,
		controlURL:          cfg.ControlURL,
		ephemeral:           cfg.Ephemeral,
		autoApproveDevices:  cfg.AutoApproveDevices,
		autoRemoveConflicts: cfg.AutoRemoveConflicts,
		log:                 cfg.Log.With().Str("services_server", cfg.Hostname).Logger(),
		ev:                  NewEventLoop[servicesCmd](64), //nolint:mnd
		certSem:             cfg.CertSem,
		tags:                cfg.Tags,
		vipAPI:              cfg.VIPServiceAPI,
		apiClient:           cfg.APIClient,
		apiFactory:          cfg.APIFactory,
		authManager:         cfg.AuthManager,
		deviceReconciler:    cfg.DeviceReconciler,
		lifecycleCfg:        cfg.LifecycleConfig,
		lifecycleProvider:   cfg.LifecycleProvider,
		whoisCache:          NewWhoisCache(whoisCacheTTL, whoisCacheMaxEntries),
	}
	go ss.loop()
	return ss
}

// sendProducer sends a command from a producer goroutine (bridge goroutine).
// It aborts if the loop has exited or the producer's context was canceled,
// preventing deadlock when the loop is blocked.
func (ss *ServicesServer) sendProducer(ctx context.Context, cmd servicesCmd) bool {
	return ss.ev.SendProducer(ctx, cmd)
}

// loop is the core event loop. It owns all mutable state.
func (ss *ServicesServer) loop() {
	defer ss.ev.Close()

	state := servicesIdle
	var rt *servicesRuntime
	var idleTimer *time.Timer
	var nextGen int

	for cmd := range ss.ev.Cmds() {
		switch cmd.(type) {
		case acquireServiceCmd, releaseServiceCmd, servicesCloseCmd, servicesIdleTimeoutCmd:
			if idleTimer != nil {
				idleTimer.Stop()
				idleTimer = nil
			}
		}

		switch c := cmd.(type) {
		case acquireServiceCmd:
			state, rt = ss.handleAcquireService(c, state, rt, &nextGen)
		case acquireServiceWorkResultCmd:
			state, rt = ss.handleAcquireWorkResult(c, state, rt)
		case releaseServiceCmd:
			state, rt = ss.handleReleaseService(c, state, rt)
			if state == servicesIdle && rt != nil {
				idleTimer = ss.scheduleIdleTimer(rt)
			}
		case servicesIdleTimeoutCmd:
			rt = ss.handleIdleTimeout(c, state, rt)
		case servicesWatchUpdateCmd:
			ss.handleWatchUpdate(c, rt)
		case servicesGetAuthURLCmd:
			ss.handleGetAuthURL(c, rt)
		case servicesCloseCmd:
			ss.handleClose(c, rt)
			return
		}
	}
}

// scheduleIdleTimer starts the idle-shutdown timer. Must be called from the loop goroutine.
func (ss *ServicesServer) scheduleIdleTimer(rt *servicesRuntime) *time.Timer {
	return ss.ev.ScheduleIdleTimer(rt.gen, servicesIdleTimeout, func(g int) servicesCmd {
		return servicesIdleTimeoutCmd{gen: g}
	})
}

func (ss *ServicesServer) handleIdleTimeout(c servicesIdleTimeoutCmd, state servicesState, rt *servicesRuntime) *servicesRuntime {
	if state == servicesIdle && rt != nil && c.gen == rt.gen {
		ss.log.Info().Msg("services server idle timeout, stopping")
		ss.stopRuntime(rt)
		return nil
	}
	return rt
}

func (ss *ServicesServer) handleClose(c servicesCloseCmd, rt *servicesRuntime) {
	if rt != nil {
		for rt.pendingCount > 0 {
			cmd := <-ss.ev.Cmds()
			switch v := cmd.(type) {
			case acquireServiceWorkResultCmd:
				_, rt = ss.handleAcquireWorkResult(v, servicesClosed, rt)
			case acquireServiceCmd:
				v.reply <- acquireServiceResult{nil, errors.New("services server closed")}
			case releaseServiceCmd:
				v.reply <- nil
			case servicesGetAuthURLCmd:
				v.reply <- ""
			case servicesCloseCmd:
				v.reply <- nil
			default:
				// servicesIdleTimeoutCmd, servicesWatchUpdateCmd have no reply channel — safe to drop.
			}
		}
		ss.stopRuntime(rt)
	}
	c.reply <- nil
}

func (ss *ServicesServer) handleAcquireService(c acquireServiceCmd, state servicesState, rt *servicesRuntime, nextGen *int) (servicesState, *servicesRuntime) {
	if state == servicesClosed {
		c.reply <- acquireServiceResult{nil, errors.New("services server closed")}
		return state, rt
	}

	if state == servicesIdle {
		if rt == nil {
			newRT := ss.startRuntime()
			if newRT == nil {
				c.reply <- acquireServiceResult{nil, errors.New("services server start failed")}
				return state, rt
			}
			newRT.gen = *nextGen
			(*nextGen)++
			rt = newRT
		}
		state = servicesRunning
	}

	key := serviceKey(c.serviceName, c.port)
	if _, exists := rt.listeners[key]; exists {
		c.reply <- acquireServiceResult{nil, fmt.Errorf("service %s port %d already acquired", c.serviceName, c.port)}
		return state, rt
	}

	allPorts := ss.collectServicePorts(rt, c.serviceName, c.port)
	gen := rt.gen
	factory := rt.factory
	tsServer := rt.tsServer
	ctx := rt.ctx

	rt.pendingCount++

	go ss.acquireServiceAsync(ctx, gen, c, allPorts, factory, tsServer)

	return state, rt
}

func (ss *ServicesServer) acquireServiceAsync(
	ctx context.Context, gen int, c acquireServiceCmd,
	allPorts []string, factory serviceListenerFactory, tsServer *tsnet.Server,
) {
	sendResult := func(listener *tsnet.ServiceListener, err error) {
		if !ss.sendProducer(ctx, acquireServiceWorkResultCmd{
			original: c,
			listener: listener,
			err:      err,
			gen:      gen,
		}) {
			if listener != nil {
				factory.Close(listener)
			}
			c.reply <- acquireServiceResult{nil, errors.New("services server closed during acquire")}
		}
	}

	ss.reconcileServiceHostname(c.serviceName)

	if err := ss.createOrUpdateVIPService(c.serviceName, allPorts); err != nil {
		sendResult(nil, fmt.Errorf("create VIP service: %w", err))
		return
	}

	var mode tsnet.ServiceMode
	if c.tcp {
		mode = tsnet.ServiceModeTCP{Port: c.port}
	} else {
		mode = tsnet.ServiceModeHTTP{Port: c.port, HTTPS: c.https}
	}

	listener, err := factory.ListenService(c.serviceName, mode)
	if err != nil {
		ss.rollbackVIPServiceOnListenFailure(c.serviceName, allPorts, c.port)
		sendResult(nil, fmt.Errorf("listen service: %w", err))
		return
	}

	if ss.autoApproveDevices {
		if err := ss.approveServiceDeviceForServer(ctx, tsServer, c.serviceName); err != nil {
			ss.log.Warn().Err(err).Str("service", c.serviceName).
				Msg("failed to auto-approve service device; manual approval required in Tailscale admin console")
		}
	}

	sendResult(listener, nil)
}

func (ss *ServicesServer) rollbackVIPServiceOnListenFailure(serviceName string, allPorts []string, failedPort uint16) {
	remaining := withoutPort(allPorts, failedPort)
	if len(remaining) == 0 {
		if delErr := ss.deleteVIPService(serviceName); delErr != nil {
			ss.log.Warn().Err(delErr).Str("service", serviceName).Msg("failed to delete VIP service after listen failure")
		}
	} else {
		if updateErr := ss.createOrUpdateVIPService(serviceName, remaining); updateErr != nil {
			ss.log.Warn().Err(updateErr).Str("service", serviceName).Msg("failed to update VIP service after listen failure")
		}
	}
}

func (ss *ServicesServer) handleAcquireWorkResult(c acquireServiceWorkResultCmd, state servicesState, rt *servicesRuntime) (servicesState, *servicesRuntime) {
	if rt != nil {
		rt.pendingCount--
	}

	if state == servicesClosed || rt == nil || c.gen != rt.gen {
		if c.listener != nil && rt != nil {
			rt.factory.Close(c.listener)
		}
		c.original.reply <- acquireServiceResult{nil, errors.New("services server closed during acquire")}
		return state, rt
	}

	if c.err != nil {
		c.original.reply <- acquireServiceResult{nil, c.err}
		return ss.maybeTransitionToIdle(state, rt)
	}

	key := serviceKey(c.original.serviceName, c.original.port)
	if _, exists := rt.listeners[key]; exists {
		rt.factory.Close(c.listener)
		c.original.reply <- acquireServiceResult{nil, fmt.Errorf("service %s port %d already acquired", c.original.serviceName, c.original.port)}
		return ss.maybeTransitionToIdle(state, rt)
	}

	rt.listeners[key] = &serviceEntry{listener: c.listener, serviceName: c.original.serviceName, port: c.original.port, https: c.original.https}
	rt.refCount++

	ss.log.Info().
		Str("service", c.original.serviceName).
		Uint16("port", c.original.port).
		Str("fqdn", c.listener.FQDN).
		Msg("service acquired")

	ss.prefetchCertForService(c, rt)

	c.original.reply <- acquireServiceResult{c.listener, nil}
	return state, rt
}

func (ss *ServicesServer) maybeTransitionToIdle(state servicesState, rt *servicesRuntime) (servicesState, *servicesRuntime) {
	if rt.refCount <= 0 && rt.pendingCount <= 0 {
		ss.stopRuntime(rt)
		return servicesIdle, nil
	}
	return state, rt
}

func (ss *ServicesServer) prefetchCertForService(c acquireServiceWorkResultCmd, rt *servicesRuntime) {
	if !rt.running || !c.original.https || c.listener == nil || c.listener.FQDN == "" {
		return
	}
	lc := ss.localClient.Load()
	if lc == nil {
		return
	}
	if !ss.validCertDomains(rt)[c.listener.FQDN] {
		return
	}
	fqdn := c.listener.FQDN
	go acquireCertForDomain(rt.ctx, lc, fqdn, ss.certSem, ss.log.With().Str("fqdn", fqdn).Logger())
}

func (ss *ServicesServer) approveServiceDeviceForServer(ctx context.Context, tsServer *tsnet.Server, serviceName string) error {
	if tsServer == nil {
		return nil
	}

	client := ss.getAPIClient()
	if client == nil {
		return errors.New("tailscale API client not configured")
	}

	lc, err := tsServer.LocalClient()
	if err != nil {
		return fmt.Errorf("get local client: %w", err)
	}

	statusCtx, cancel := context.WithTimeout(ctx, apiTimeout)
	defer cancel()

	status, err := lc.Status(statusCtx)
	if err != nil {
		return fmt.Errorf("get node status: %w", err)
	}
	if status.Self == nil {
		return errors.New("no self in node status")
	}

	nodeID := string(status.Self.ID)
	if nodeID == "" {
		return errors.New("empty node ID in status")
	}

	client.Services()

	u := client.BaseURL.JoinPath("api", "v2", "tailnet", "-", "services", serviceName, "device", nodeID, "approved")

	body := `{"approved":true}`
	req, err := http.NewRequestWithContext(statusCtx, http.MethodPost, u.String(), strings.NewReader(body))
	if err != nil {
		return fmt.Errorf("create approval request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("approval request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			return fmt.Errorf("approval failed (HTTP %d): unable to read response body: %w", resp.StatusCode, readErr)
		}
		return fmt.Errorf("approval failed (HTTP %d): %s", resp.StatusCode, string(respBody))
	}

	ss.log.Info().Str("service", serviceName).Str("nodeID", nodeID).Msg("service device approved")
	return nil
}

func (ss *ServicesServer) handleReleaseService(c releaseServiceCmd, state servicesState, rt *servicesRuntime) (servicesState, *servicesRuntime) {
	c.reply <- nil

	if state != servicesRunning || rt == nil {
		return state, rt
	}

	key := serviceKey(c.serviceName, c.port)
	entry, ok := rt.listeners[key]
	if !ok {
		if rt.refCount <= 0 {
			return servicesIdle, rt
		}
		return state, rt
	}

	rt.factory.Close(entry.listener)
	delete(rt.listeners, key)
	rt.refCount--

	remainingPorts := ss.collectExistingServicePorts(rt, c.serviceName)
	if len(remainingPorts) == 0 {
		if err := ss.deleteVIPService(c.serviceName); err != nil {
			ss.log.Warn().Err(err).Str("service", c.serviceName).Msg("failed to delete VIP service")
		}
	} else {
		if err := ss.createOrUpdateVIPService(c.serviceName, remainingPorts); err != nil {
			ss.log.Warn().Err(err).Str("service", c.serviceName).Msg("failed to update VIP service after port release")
		}
	}

	ss.log.Info().
		Str("service", c.serviceName).
		Uint16("port", c.port).
		Msg("service released")

	if rt.refCount <= 0 {
		return servicesIdle, rt
	}
	return state, rt
}

// Acquire creates a VIP Service and returns a ServiceListener for it.
func (ss *ServicesServer) Acquire(serviceName string, port uint16, https, tcp bool) (*tsnet.ServiceListener, error) {
	cmd := acquireServiceCmd{
		reply:       make(chan acquireServiceResult, 1),
		serviceName: serviceName,
		port:        port,
		https:       https,
		tcp:         tcp,
	}

	result, ok := SendAndWait[servicesCmd, acquireServiceResult](ss.ev, cmd, cmd.reply)
	if !ok {
		return nil, errors.New("services server closed")
	}
	return result.listener, result.err
}

// Release closes a service listener and deletes the VIP Service.
func (ss *ServicesServer) Release(serviceName string, port uint16) error {
	cmd := releaseServiceCmd{
		reply:       make(chan error, 1),
		serviceName: serviceName,
		port:        port,
	}
	_, ok := SendAndWait[servicesCmd, error](ss.ev, cmd, cmd.reply)
	if !ok {
		return errors.New("services server closed")
	}
	return nil
}

// Close shuts down the services server permanently.
func (ss *ServicesServer) Close() {
	if ss.ev.IsClosed() {
		return
	}
	cmd := servicesCloseCmd{reply: make(chan error, 1)}
	_, _ = SendAndWait[servicesCmd, error](ss.ev, cmd, cmd.reply)
}

// Whois resolves identity from the request. For VIP-proxied requests the
// RemoteAddr is localhost, but X-Forwarded-For carries the original peer's
// Tailscale IP (set by the VIP proxy's addProxyForwardedHeaders). Falls
// back to empty identity when neither path succeeds.
func (ss *ServicesServer) Whois(r *http.Request) model.Whois {
	if r == nil {
		return model.Whois{}
	}

	lc := ss.localClient.Load()
	if lc == nil {
		return model.Whois{}
	}

	peerIP := ss.trustedPeerIP(r)

	ss.log.Debug().
		Bool("has_local_client", lc != nil).
		Str("remote_addr", r.RemoteAddr).
		Bool("is_localhost", isLocalhost(r.RemoteAddr)).
		Str("x_forwarded_for", r.Header.Get("X-Forwarded-For")).
		Str("peer_ip", peerIP).
		Msg("service whois")

	if peerIP == "" {
		return model.Whois{}
	}

	return cachedWhoisFromAddr(r.Context(), ss.whoisCache, lc, peerIP)
}

// trustedPeerIP derives the peer IP to use for Whois lookup.
// For non-localhost RemoteAddr the address is returned directly.
// For localhost (VIP proxy hop), only a single valid IP in X-Forwarded-For
// is accepted — anything else returns "" to avoid a doomed WhoIs(127.0.0.1).
//
// Security note: the XFF trust boundary assumes that no untrusted process
// can reach the VIP service listener on loopback. If the tsnet listener is
// ever exposed beyond the local machine, XFF spoofing becomes possible.
func (ss *ServicesServer) trustedPeerIP(r *http.Request) string {
	if !isLocalhost(r.RemoteAddr) {
		return NormalizeIP(r.RemoteAddr)
	}

	// Reject if multiple X-Forwarded-For headers are present — a single
	// trusted proxy hop should produce exactly one header. Multiple
	// headers indicate header spoofing (Go's Header.Get merges them
	// with commas, hiding the attack).
	xffVals := r.Header.Values("X-Forwarded-For")
	if len(xffVals) != 1 {
		return ""
	}

	xff := xffVals[0]

	// Only trust a single IP in XFF — a comma-separated chain is
	// unexpected from the VIP proxy's addProxyForwardedHeaders.
	if strings.IndexByte(xff, ',') >= 0 {
		return ""
	}

	ip := NormalizeIP(strings.TrimSpace(xff))
	if ip == "" {
		return ""
	}

	// Reject loopback IPs from XFF — a loopback value means either a
	// misconfigured proxy or a spoof attempt. Either way, WhoIs would fail.
	if parsed := net.ParseIP(ip); parsed != nil && parsed.IsLoopback() {
		return ""
	}

	return ip
}

func isLocalhost(remoteAddr string) bool {
	return model.IsLocalhost(remoteAddr)
}

// startRuntime creates a new tsnet.Server and starts it.
// When lifecycleCfg is set, uses NodeLifecycle for full lifecycle management.
func (ss *ServicesServer) startRuntime() *servicesRuntime {
	return ss.startRuntimeWithLifecycle()
}

func (ss *ServicesServer) startRuntimeWithLifecycle() *servicesRuntime {
	if ss.lifecycleCfg == nil {
		ss.log.Error().Msg("startRuntimeWithLifecycle called with nil lifecycleCfg")
		return nil
	}

	provider := ss.lifecycleProvider
	if provider == nil {
		provider = DefaultNodeLifecycleProvider
	}

	startCtx, startCancel := context.WithTimeout(context.Background(), apiTimeout*3) //nolint:mnd
	defer startCancel()
	lifecycle, nodeRt, svcFactory, err := provider(startCtx, ss.log.With().Str("component", "lifecycle").Logger(), *ss.lifecycleCfg)
	if err != nil {
		ss.log.Error().Err(err).Msg("failed to start services tsnet server via lifecycle")
		return nil
	}

	if nodeRt.LocalClient != nil {
		ss.localClient.Store(nodeRt.LocalClient)
	}

	if svcFactory == nil {
		if nodeRt.Server == nil {
			ss.log.Error().Msg("lifecycle provider returned nil both factory and server")
			// Clean up the started lifecycle to avoid goroutine/resource leaks.
			lifecycle.Close()
			return nil
		}
		svcFactory = tsnetServerFactory{nodeRt.Server}
	}

	rt := &servicesRuntime{
		ctx:       nodeRt.Ctx,
		cancel:    nodeRt.Cancel,
		tsServer:  nodeRt.Server,
		listeners: make(map[string]*serviceEntry),
		factory:   svcFactory,
		lifecycle: lifecycle,
	}

	bridgeDone := make(chan struct{})
	rt.bridgeDone = bridgeDone
	go func() {
		defer close(bridgeDone)
		ss.bridgeLifecycleEvents(nodeRt.Ctx, lifecycle)
	}()

	return rt
}

// stopRuntime tears down the tsnet server and all listeners.
func (ss *ServicesServer) stopRuntime(rt *servicesRuntime) {
	// Collect unique service names to avoid redundant API delete calls.
	seen := make(map[string]bool)
	for key, entry := range rt.listeners {
		rt.factory.Close(entry.listener)
		if !seen[entry.serviceName] {
			if err := ss.deleteVIPService(entry.serviceName); err != nil {
				ss.log.Warn().Err(err).Str("service", entry.serviceName).Msg("failed to delete VIP service during shutdown")
			}
			seen[entry.serviceName] = true
		}
		delete(rt.listeners, key)
	}

	if rt.lifecycle != nil {
		if err := rt.lifecycle.Close(); err != nil {
			ss.log.Warn().Err(err).Msg("error stopping lifecycle")
		}
	}

	if rt.cancel != nil {
		rt.cancel()
	}

	if rt.bridgeDone != nil {
		<-rt.bridgeDone
	}

	// Clear stale localClient so Whois() doesn't use a closed client.
	ss.localClient.Store(nil)
	// Clear stale authURL from previous runtime.
	ss.authURL = ""
}

func (ss *ServicesServer) bridgeLifecycleEvents(ctx context.Context, lifecycle *NodeLifecycle) {
	events := lifecycle.WatchEvents()
	for evt := range events {
		if ctx.Err() != nil {
			return
		}
		ss.sendProducer(ctx, servicesWatchUpdateCmd{
			authURL: evt.AuthURL,
			status:  evt.Status,
		})
	}
}

func (ss *ServicesServer) handleWatchUpdate(c servicesWatchUpdateCmd, rt *servicesRuntime) {
	if c.authURL != "" {
		ss.authURL = c.authURL
		if rt != nil {
			rt.authURL = c.authURL
		}
	}
	if rt == nil {
		return
	}
	if c.status == model.ProxyStatusRunning && !rt.running {
		rt.running = true
		ss.prefetchCerts(rt)
	}
}

func (ss *ServicesServer) prefetchCerts(rt *servicesRuntime) {
	lc := ss.localClient.Load()
	if lc == nil {
		return
	}

	validDomains := ss.validCertDomains(rt)

	for _, entry := range rt.listeners {
		if !entry.https || entry.listener == nil || entry.listener.FQDN == "" {
			continue
		}
		if !validDomains[entry.listener.FQDN] {
			ss.log.Debug().Str("fqdn", entry.listener.FQDN).
				Msg("skipping cert prefetch: domain not yet in CertDomains")
			continue
		}
		fqdn := entry.listener.FQDN
		go acquireCertForDomain(rt.ctx, lc, fqdn, ss.certSem, ss.log.With().Str("fqdn", fqdn).Logger())
	}
}

func (ss *ServicesServer) validCertDomains(rt *servicesRuntime) map[string]bool {
	certDomains := rt.tsServer.CertDomains()
	set := make(map[string]bool, len(certDomains))
	for _, d := range certDomains {
		set[d] = true
	}
	return set
}

func (ss *ServicesServer) handleGetAuthURL(c servicesGetAuthURLCmd, rt *servicesRuntime) {
	if rt != nil && rt.authURL != "" {
		c.reply <- rt.authURL
		return
	}
	c.reply <- ss.authURL
}

// GetAuthURL returns the current auth URL, or empty string if not needed.
func (ss *ServicesServer) GetAuthURL() string {
	cmd := servicesGetAuthURLCmd{reply: make(chan string, 1)}
	v, ok := SendAndWait[servicesCmd, string](ss.ev, cmd, cmd.reply)
	if !ok {
		return ""
	}
	return v
}

func (ss *ServicesServer) getAPIClient() *tailscale.Client {
	if ss.apiClient != nil {
		return ss.apiClient
	}
	if ss.apiFactory != nil {
		return ss.apiFactory.NewClient(ScopesServices()...)
	}
	return nil
}

// createOrUpdateVIPService creates or updates a VIP Service via the Tailscale API.
func (ss *ServicesServer) createOrUpdateVIPService(serviceName string, ports []string) error {
	if api := ss.vipAPI; api != nil {
		return api.createOrUpdateVIPService(serviceName, ports)
	}
	return ss.createOrUpdateVIPServiceProd(serviceName, ports)
}

func (ss *ServicesServer) createOrUpdateVIPServiceProd(serviceName string, ports []string) error {
	client := ss.getAPIClient()
	if client == nil {
		return errors.New("tailscale API client not configured")
	}

	// Tailscale VIP Service API requires existing Addrs in PUT updates.
	// First attempt a simple create; if the API rejects it for missing Addrs,
	// GET the existing service and retry with Addrs included.
	firstCtx, firstCancel := context.WithTimeout(context.Background(), apiTimeout)
	err := client.Services().CreateOrUpdate(firstCtx, tailscale.VIPService{
		Name:  serviceName,
		Ports: ports,
		Tags:  cleanTags(ss.tags),
	})
	firstCancel()

	if err == nil {
		return nil
	}

	// Handle 409 "name is in use but is not a service": a regular device with
	// the same hostname blocks VIP service creation. When autoRemoveConflicts
	// is enabled, delete the conflicting device and retry.
	if ss.autoRemoveConflicts && isNameInUseError(err) {
		ss.log.Info().
			Str("service", serviceName).
			Msg("VIP service name conflict detected, attempting auto-removal of conflicting device")

		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), apiTimeout)
		cleanupErr := ss.removeConflictingDevice(cleanupCtx, client, serviceName)
		cleanupCancel()

		if cleanupErr != nil {
			return fmt.Errorf("auto-remove conflicting device for %q: %w (original: %w)", serviceName, cleanupErr, err)
		}

		// Retry after cleanup.
		retryCtx, retryCancel := context.WithTimeout(context.Background(), apiTimeout)
		defer retryCancel()
		return client.Services().CreateOrUpdate(retryCtx, tailscale.VIPService{
			Name:  serviceName,
			Ports: ports,
			Tags:  cleanTags(ss.tags),
		})
	}

	if !strings.Contains(err.Error(), "addrs must contain") {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), apiTimeout)
	defer cancel()

	existing, getErr := client.Services().Get(ctx, serviceName)
	if getErr != nil {
		if !isNotFound(getErr) {
			return fmt.Errorf("get existing VIP service %q: %w", serviceName, getErr)
		}
		// Service confirmed not-found — create directly without delete.
		ss.log.Debug().Str("service", serviceName).Msg("VIP service not found, creating fresh")
		createCtx, createCancel := context.WithTimeout(context.Background(), apiTimeout)
		defer createCancel()
		return client.Services().CreateOrUpdate(createCtx, tailscale.VIPService{
			Name:  serviceName,
			Ports: ports,
			Tags:  cleanTags(ss.tags),
		})
	}

	updateCtx, updateCancel := context.WithTimeout(context.Background(), apiTimeout)
	defer updateCancel()
	return client.Services().CreateOrUpdate(updateCtx, tailscale.VIPService{
		Name:  serviceName,
		Addrs: existing.Addrs,
		Ports: ports,
		Tags:  cleanTags(ss.tags),
	})
}

func (ss *ServicesServer) reconcileServiceHostname(serviceName string) {
	hostname := strings.TrimPrefix(serviceName, "svc:")
	if hostname == serviceName {
		return
	}

	if ss.deviceReconciler != nil {
		ctx, cancel := context.WithTimeout(context.Background(), apiTimeout)
		defer cancel()
		ss.deviceReconciler.Reconcile(ctx, hostname, ss.tags, nil)
		return
	}

	ss.log.Debug().Str("service", serviceName).Msg("device reconciler not available, skipping hostname cleanup")
}

// deleteVIPService deletes a VIP Service via the Tailscale API.
func (ss *ServicesServer) deleteVIPService(serviceName string) error {
	if api := ss.vipAPI; api != nil {
		return api.deleteVIPService(serviceName)
	}
	return ss.deleteVIPServiceProd(serviceName)
}

func (ss *ServicesServer) deleteVIPServiceProd(serviceName string) error {
	ctx, cancel := context.WithTimeout(context.Background(), apiTimeout)
	defer cancel()

	client := ss.getAPIClient()
	if client == nil {
		return errors.New("tailscale API client not configured")
	}

	return client.Services().Delete(ctx, serviceName)
}

// collectServicePorts gathers all known ports for a service from active listeners,
// plus the new port being acquired.
func (ss *ServicesServer) collectServicePorts(rt *servicesRuntime, serviceName string, newPort uint16) []string {
	seen := map[uint16]bool{newPort: true}
	for _, entry := range rt.listeners {
		if entry.serviceName == serviceName {
			seen[entry.port] = true
		}
	}
	ports := make([]string, 0, len(seen))
	for p := range seen {
		ports = append(ports, fmt.Sprintf("tcp:%d", p))
	}
	sort.Slice(ports, func(i, j int) bool { return ports[i] < ports[j] })
	return ports
}

// collectExistingServicePorts returns port specs for all active listeners of a service.
func (ss *ServicesServer) collectExistingServicePorts(rt *servicesRuntime, serviceName string) []string {
	var ports []string
	for _, entry := range rt.listeners {
		if entry.serviceName == serviceName {
			ports = append(ports, fmt.Sprintf("tcp:%d", entry.port))
		}
	}
	sort.Slice(ports, func(i, j int) bool { return ports[i] < ports[j] })
	return ports
}

func withoutPort(ports []string, port uint16) []string {
	target := fmt.Sprintf("tcp:%d", port)
	filtered := make([]string, 0, len(ports))
	for _, p := range ports {
		if p != target {
			filtered = append(filtered, p)
		}
	}
	return filtered
}

// serviceKey builds a map key from service name and port.
func serviceKey(serviceName string, port uint16) string {
	return fmt.Sprintf("%s:%d", serviceName, port)
}

// isNotFound returns true if the error indicates the requested resource
// does not exist (HTTP 404 or equivalent). Used to distinguish confirmed
// not-found from transient API failures.
func isNotFound(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "not found") || strings.Contains(msg, "404")
}

// isNameInUseError returns true if the error indicates the VIP service name
// is already taken by a non-service device (HTTP 409).
func isNameInUseError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "name is in use") || strings.Contains(msg, "409")
}

// removeConflictingDevice finds and deletes Tailscale devices whose hostname
// matches the service name, bypassing the tag filter and online-status checks
// used by the normal DeviceReconciler. This is needed to resolve 409 errors
// where a stale regular device blocks VIP service creation.
func (ss *ServicesServer) removeConflictingDevice(ctx context.Context, client *tailscale.Client, serviceName string) error {
	hostname := strings.TrimPrefix(serviceName, "svc:")
	if hostname == serviceName {
		return nil
	}

	devices, err := client.Devices().List(ctx)
	if err != nil {
		return fmt.Errorf("list devices: %w", err)
	}

	for _, d := range devices {
		if d.Hostname != hostname {
			continue
		}

		ss.log.Info().
			Str("hostname", d.Hostname).
			Str("node_id", d.NodeID).
			Bool("was_online", d.ConnectedToControl).
			Msg("auto-removing conflicting device for VIP service")

		if deleteErr := client.Devices().Delete(ctx, d.NodeID); deleteErr != nil {
			return fmt.Errorf("delete conflicting device %q (node %s): %w", d.Hostname, d.NodeID, deleteErr)
		}
	}

	return nil
}
