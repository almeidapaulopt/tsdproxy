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
	Log                zerolog.Logger
	VIPServiceAPI      vipServiceAPI
	ListenerFactory    serviceListenerFactory
	APIClient          *tailscale.Client
	APIFactory         *APIClientFactory
	AuthManager        *AuthManager
	DeviceReconciler   *DeviceReconciler
	LifecycleConfig    *NodeLifecycleConfig
	Hostname           string
	DataDir            string
	AuthKey            string
	ControlURL         string
	Tags               string
	Ephemeral          bool
	AutoApproveDevices bool
}

// serviceEntry tracks a single service listener.
type serviceEntry struct {
	listener    *tsnet.ServiceListener
	serviceName string
	port        uint16
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
	factory   serviceListenerFactory
	cancel    context.CancelFunc
	tsServer  *tsnet.Server
	listeners map[string]*serviceEntry
	lifecycle *NodeLifecycle
	refCount  int
	gen       int
}

// Command types for the state machine.

type servicesCmd interface{ svcCmd() }

type acquireServiceCmd struct {
	reply       chan acquireServiceResult
	serviceName string
	port        uint16
	https       bool
	tcp         bool
}

func (acquireServiceCmd) svcCmd() {}

type acquireServiceResult struct {
	listener *tsnet.ServiceListener
	err      error
}

type releaseServiceCmd struct {
	reply       chan error
	serviceName string
	port        uint16
}

func (releaseServiceCmd) svcCmd() {}

type servicesCloseCmd struct {
	reply chan error
}

func (servicesCloseCmd) svcCmd() {}

type servicesIdleTimeoutCmd struct {
	gen int
}

func (servicesIdleTimeoutCmd) svcCmd() {}

type servicesWatchUpdateCmd struct {
	authURL string
}

func (servicesWatchUpdateCmd) svcCmd() {}

type servicesGetAuthURLCmd struct {
	reply chan string
}

func (servicesGetAuthURLCmd) svcCmd() {}

// ServicesServer manages a shared, ref-counted tsnet.Server for services mode.
// All mutable state is managed by a single event-loop goroutine.
type ServicesServer struct {
	log                zerolog.Logger
	listenerFactory    serviceListenerFactory
	vipAPI             vipServiceAPI
	localClient        atomic.Pointer[local.Client]
	apiClient          *tailscale.Client
	apiFactory         *APIClientFactory
	authManager        *AuthManager
	deviceReconciler   *DeviceReconciler
	lifecycleCfg       *NodeLifecycleConfig
	cmds               chan servicesCmd
	done               chan struct{}
	whoisCache         *WhoisCache
	controlURL         string
	datadir            string
	hostname           string
	tags               string
	authKey            string
	authURL            string
	closed             atomic.Bool
	ephemeral          bool
	autoApproveDevices bool
}

// NewServicesServer creates a ServicesServer and starts the event loop.
func NewServicesServer(cfg ServicesServerConfig) *ServicesServer {
	ss := &ServicesServer{
		hostname:           cfg.Hostname,
		datadir:            cfg.DataDir,
		authKey:            cfg.AuthKey,
		controlURL:         cfg.ControlURL,
		ephemeral:          cfg.Ephemeral,
		autoApproveDevices: cfg.AutoApproveDevices,
		log:                cfg.Log.With().Str("services_server", cfg.Hostname).Logger(),
		cmds:               make(chan servicesCmd, 64), //nolint:mnd
		done:               make(chan struct{}),
		tags:               cfg.Tags,
		vipAPI:             cfg.VIPServiceAPI,
		listenerFactory:    cfg.ListenerFactory,
		apiClient:          cfg.APIClient,
		apiFactory:         cfg.APIFactory,
		authManager:        cfg.AuthManager,
		deviceReconciler:   cfg.DeviceReconciler,
		lifecycleCfg:       cfg.LifecycleConfig,
		whoisCache:         NewWhoisCache(whoisCacheTTL, whoisCacheMaxEntries),
	}
	go ss.loop()
	return ss
}

// sendProducer sends a command from a producer goroutine (bridge goroutine).
// It aborts if the loop has exited or the producer's context was canceled,
// preventing deadlock when the loop is blocked.
func (ss *ServicesServer) sendProducer(ctx context.Context, cmd servicesCmd) bool {
	select {
	case ss.cmds <- cmd:
		return true
	case <-ss.done:
		return false
	case <-ctx.Done():
		return false
	}
}

// sendPublic sends a command from a public method.
func (ss *ServicesServer) sendPublic(cmd servicesCmd) bool {
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

// servicesSendAndWait sends a command via the event loop and waits for a reply.
func servicesSendAndWait[T any](ss *ServicesServer, cmd servicesCmd, reply chan T) (T, bool) {
	if !ss.sendPublic(cmd) {
		var zero T
		return zero, false
	}
	select {
	case v := <-reply:
		return v, true
	case <-ss.done:
		var zero T
		return zero, false
	}
}

// loop is the core event loop. It owns all mutable state.
func (ss *ServicesServer) loop() {
	defer close(ss.done)

	state := servicesIdle
	var rt *servicesRuntime
	var idleTimer *time.Timer
	var nextGen int

	for cmd := range ss.cmds {
		switch cmd.(type) {
		case acquireServiceCmd, releaseServiceCmd, servicesCloseCmd, servicesIdleTimeoutCmd:
			if idleTimer != nil {
				idleTimer.Stop()
				idleTimer = nil
			}
		}

		switch c := cmd.(type) {
		case acquireServiceCmd:
			state, rt = ss.handleAcquireService(c, state, rt)
			if rt != nil && rt.gen == 0 {
				rt.gen = nextGen
				nextGen++
			}
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
			if rt != nil {
				ss.stopRuntime(rt)
			}
			ss.closed.Store(true)
			c.reply <- nil
			return
		}
	}
}

// scheduleIdleTimer starts the idle-shutdown timer. Must be called from the loop goroutine.
func (ss *ServicesServer) scheduleIdleTimer(rt *servicesRuntime) *time.Timer {
	capturedGen := rt.gen
	return time.AfterFunc(servicesIdleTimeout, func() {
		select {
		case ss.cmds <- servicesIdleTimeoutCmd{gen: capturedGen}:
		case <-ss.done:
		}
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

func (ss *ServicesServer) handleAcquireService(c acquireServiceCmd, state servicesState, rt *servicesRuntime) (servicesState, *servicesRuntime) {
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
			rt = newRT
		}
		state = servicesRunning
	}

	key := serviceKey(c.serviceName, c.port)
	if _, exists := rt.listeners[key]; exists {
		c.reply <- acquireServiceResult{nil, fmt.Errorf("service %s port %d already acquired", c.serviceName, c.port)}
		return state, rt
	}

	// Collect all known ports for this service (including the new one).
	allPorts := ss.collectServicePorts(rt, c.serviceName, c.port)

	// Create or update VIP Service via API with ALL ports.
	if err := ss.createOrUpdateVIPService(c.serviceName, allPorts); err != nil {
		c.reply <- acquireServiceResult{nil, fmt.Errorf("create VIP service: %w", err)}
		if rt.refCount <= 0 {
			ss.stopRuntime(rt)
			rt = nil
			state = servicesIdle
		}
		return state, rt
	}

	// Determine the service mode.
	var mode tsnet.ServiceMode
	if c.tcp {
		mode = tsnet.ServiceModeTCP{Port: c.port}
	} else {
		mode = tsnet.ServiceModeHTTP{Port: c.port, HTTPS: c.https}
	}

	listener, err := rt.factory.ListenService(c.serviceName, mode)
	if err != nil {
		ss.reconcileVIPServiceOnFailure(rt, c.serviceName)
		c.reply <- acquireServiceResult{nil, fmt.Errorf("listen service: %w", err)}
		if rt.refCount <= 0 {
			ss.stopRuntime(rt)
			rt = nil
			state = servicesIdle
		}
		return state, rt
	}

	rt.listeners[key] = &serviceEntry{listener: listener, serviceName: c.serviceName, port: c.port}
	rt.refCount++

	if ss.autoApproveDevices {
		if err := ss.approveServiceDevice(rt, c.serviceName); err != nil {
			ss.log.Warn().Err(err).Str("service", c.serviceName).
				Msg("failed to auto-approve service device; manual approval required in Tailscale admin console")
		}
	}

	ss.log.Info().
		Str("service", c.serviceName).
		Uint16("port", c.port).
		Str("fqdn", listener.FQDN).
		Msg("service acquired")

	c.reply <- acquireServiceResult{listener, nil}
	return state, rt
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

	result, ok := servicesSendAndWait(ss, cmd, cmd.reply)
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
	_, ok := servicesSendAndWait(ss, cmd, cmd.reply)
	if !ok {
		return errors.New("services server closed")
	}
	return nil
}

// Close shuts down the services server permanently.
func (ss *ServicesServer) Close() {
	if ss.closed.Load() {
		return
	}
	cmd := servicesCloseCmd{reply: make(chan error, 1)}
	_, _ = servicesSendAndWait(ss, cmd, cmd.reply)
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
	if ss.lifecycleCfg != nil {
		return ss.startRuntimeWithLifecycle()
	}
	return ss.startRuntimeLegacy()
}

func (ss *ServicesServer) startRuntimeWithLifecycle() *servicesRuntime {
	// Test mode: use injected listener factory without creating a real tsnet.Server.
	if ss.listenerFactory != nil {
		_, cancel := context.WithCancel(context.Background())
		return &servicesRuntime{
			cancel:    cancel,
			listeners: make(map[string]*serviceEntry),
			factory:   ss.listenerFactory,
		}
	}

	lifecycle := NewNodeLifecycle(
		ss.log.With().Str("component", "lifecycle").Logger(),
		*ss.lifecycleCfg,
	)

	nodeRt, err := lifecycle.Start(context.Background())
	if err != nil {
		ss.log.Error().Err(err).Msg("failed to start services tsnet server via lifecycle")
		return nil
	}

	if lc, err := nodeRt.Server.LocalClient(); err == nil {
		ss.localClient.Store(lc)
	}

	rt := &servicesRuntime{
		cancel:    nodeRt.Cancel,
		tsServer:  nodeRt.Server,
		listeners: make(map[string]*serviceEntry),
		factory:   tsnetServerFactory{nodeRt.Server},
		lifecycle: lifecycle,
	}

	go ss.bridgeLifecycleEvents(nodeRt.Ctx, lifecycle)

	return rt
}

// startRuntimeLegacy is the startup path for tests that inject a listenerFactory
// without a full NodeLifecycle. Production always uses startRuntimeWithLifecycle.
func (ss *ServicesServer) startRuntimeLegacy() *servicesRuntime {
	_, cancel := context.WithCancel(context.Background())

	// Test mode: use injected listener factory without creating a real tsnet.Server.
	if ss.listenerFactory != nil {
		return &servicesRuntime{
			cancel:    cancel,
			listeners: make(map[string]*serviceEntry),
			factory:   ss.listenerFactory,
		}
	}

	controlURL := ss.controlURL
	if controlURL == "" {
		controlURL = model.DefaultTailscaleControlURL
	}

	allTags := cleanTags(ss.tags)
	var advertiseTag []string
	if len(allTags) > 0 {
		advertiseTag = []string{allTags[0]}
	}

	tsServer := &tsnet.Server{
		Hostname:      ss.hostname,
		AuthKey:       ss.authKey,
		Dir:           ss.datadir,
		Ephemeral:     ss.ephemeral,
		AdvertiseTags: advertiseTag,
		ControlURL:    controlURL,
		UserLogf:      func(format string, args ...any) { ss.log.Info().Msgf(format, args...) },
		Logf:          func(format string, args ...any) { ss.log.Trace().Msgf(format, args...) },
	}

	// Generate a fresh OAuth auth key on each startup. The OAuth key is one-time
	// (Reusable=false), so the cached ss.authKey is stale after the first use.
	// A fresh key ensures tsnet can authenticate even if the node's saved state
	// requires re-auth (e.g. tailscaled.state was deleted or is stale).
	if ss.authManager != nil {
		if newKey, err := ss.authManager.GenerateOAuthKey(context.Background(), ss.tags); err != nil {
			ss.log.Warn().Err(err).Msg("failed to generate fresh auth key, using cached key")
		} else if newKey != "" {
			tsServer.AuthKey = newKey
			ss.log.Debug().Msg("using freshly generated OAuth auth key")
		}
	}

	if ss.deviceReconciler != nil {
		reconcileCtx, reconcileCancel := context.WithTimeout(context.Background(), 30*time.Second) //nolint:mnd
		ss.deviceReconciler.Reconcile(reconcileCtx, ss.hostname, ss.tags)
		reconcileCancel()
	}

	if err := tsServer.Start(); err != nil {
		ss.log.Error().Err(err).Msg("failed to start services tsnet server")
		cancel()
		return nil
	}

	if lc, err := tsServer.LocalClient(); err == nil {
		ss.localClient.Store(lc)
	}

	return &servicesRuntime{
		cancel:    cancel,
		tsServer:  tsServer,
		listeners: make(map[string]*serviceEntry),
		factory:   tsnetServerFactory{tsServer},
	}
}

// stopRuntime tears down the tsnet server and all listeners.
func (ss *ServicesServer) stopRuntime(rt *servicesRuntime) {
	// Collect unique service names to avoid redundant API delete calls.
	seen := make(map[string]bool)
	for key, entry := range rt.listeners {
		rt.factory.Close(entry.listener)
		if !seen[entry.serviceName] {
			_ = ss.deleteVIPService(entry.serviceName)
			seen[entry.serviceName] = true
		}
		delete(rt.listeners, key)
	}

	if rt.lifecycle != nil {
		if err := rt.lifecycle.Close(); err != nil {
			ss.log.Warn().Err(err).Msg("error stopping lifecycle")
		}
	} else if rt.tsServer != nil {
		rt.tsServer.Close()
	}

	if rt.cancel != nil {
		rt.cancel()
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
		if evt.AuthURL != "" {
			ss.sendProducer(ctx, servicesWatchUpdateCmd{authURL: evt.AuthURL})
		}
	}
}

func (ss *ServicesServer) handleWatchUpdate(c servicesWatchUpdateCmd, _ *servicesRuntime) {
	if c.authURL != "" {
		ss.authURL = c.authURL
	}
}

func (ss *ServicesServer) handleGetAuthURL(c servicesGetAuthURLCmd, _ *servicesRuntime) {
	c.reply <- ss.authURL
}

// GetAuthURL returns the current auth URL, or empty string if not needed.
func (ss *ServicesServer) GetAuthURL() string {
	cmd := servicesGetAuthURLCmd{reply: make(chan string, 1)}
	v, ok := servicesSendAndWait(ss, cmd, cmd.reply)
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
	firstCtx, firstCancel := context.WithTimeout(context.Background(), 30*time.Second) //nolint:mnd
	err := client.VIPServices().CreateOrUpdate(firstCtx, tailscale.VIPService{
		Name:  serviceName,
		Ports: ports,
		Tags:  cleanTags(ss.tags),
	})
	firstCancel()
	if err == nil || !strings.Contains(err.Error(), "addrs must contain") {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second) //nolint:mnd
	defer cancel()

	existing, getErr := client.VIPServices().Get(ctx, serviceName)
	if getErr != nil {
		if !isNotFound(getErr) {
			return fmt.Errorf("get existing VIP service %q: %w", serviceName, getErr)
		}
		// Service confirmed not-found — create directly without delete.
		ss.log.Debug().Str("service", serviceName).Msg("VIP service not found, creating fresh")
		createCtx, createCancel := context.WithTimeout(context.Background(), 30*time.Second) //nolint:mnd
		defer createCancel()
		return client.VIPServices().CreateOrUpdate(createCtx, tailscale.VIPService{
			Name:  serviceName,
			Ports: ports,
			Tags:  cleanTags(ss.tags),
		})
	}

	updateCtx, updateCancel := context.WithTimeout(context.Background(), 30*time.Second) //nolint:mnd
	defer updateCancel()
	return client.VIPServices().CreateOrUpdate(updateCtx, tailscale.VIPService{
		Name:  serviceName,
		Addrs: existing.Addrs,
		Ports: ports,
		Tags:  cleanTags(ss.tags),
	})
}

// deleteVIPService deletes a VIP Service via the Tailscale API.
func (ss *ServicesServer) deleteVIPService(serviceName string) error {
	if api := ss.vipAPI; api != nil {
		return api.deleteVIPService(serviceName)
	}
	return ss.deleteVIPServiceProd(serviceName)
}

func (ss *ServicesServer) deleteVIPServiceProd(serviceName string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second) //nolint:mnd
	defer cancel()

	client := ss.getAPIClient()
	if client == nil {
		return errors.New("tailscale API client not configured")
	}

	return client.VIPServices().Delete(ctx, serviceName)
}

func (ss *ServicesServer) approveServiceDevice(rt *servicesRuntime, serviceName string) error {
	if rt.tsServer == nil {
		return nil
	}

	client := ss.getAPIClient()
	if client == nil {
		return errors.New("tailscale API client not configured")
	}

	lc, err := rt.tsServer.LocalClient()
	if err != nil {
		return fmt.Errorf("get local client: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second) //nolint:mnd
	defer cancel()

	status, err := lc.Status(ctx)
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

	// Trigger client lazy init (sets BaseURL from default).
	client.VIPServices()

	// Endpoint: POST /api/v2/tailnet/{tailnet}/services/{svc}/device/{nodeId}/approved
	u := client.BaseURL.JoinPath("api", "v2", "tailnet", "-", "services", serviceName, "device", nodeID, "approved")

	body := `{"approved":true}`
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), strings.NewReader(body))
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

// reconcileVIPServiceOnFailure rolls back the VIP service port list after a
// ListenService failure. If other ports exist for the service, updates the
// VIP service with only the remaining ports. Otherwise deletes the VIP service.
func (ss *ServicesServer) reconcileVIPServiceOnFailure(rt *servicesRuntime, serviceName string) {
	existingPorts := ss.collectExistingServicePorts(rt, serviceName)
	if len(existingPorts) == 0 {
		_ = ss.deleteVIPService(serviceName)
	} else {
		if err := ss.createOrUpdateVIPService(serviceName, existingPorts); err != nil {
			ss.log.Warn().Err(err).Str("service", serviceName).Msg("failed to reconcile VIP service after listen failure")
		}
	}
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
