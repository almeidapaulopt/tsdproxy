// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package tailscale

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog"
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
	Log              zerolog.Logger
	Hostname         string
	DataDir          string
	AuthKey          string
	ControlURL       string
	ClientID         string
	ClientSecret     string
	Tags             string
	Ephemeral        bool
	VIPServiceAPI    vipServiceAPI          // optional, nil = production default
	ListenerFactory  serviceListenerFactory // optional, nil = production default
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
	cancel    context.CancelFunc
	tsServer  *tsnet.Server            // nil in test mode
	listeners map[string]*serviceEntry // "serviceName:port" → entry
	refCount  int
	factory   serviceListenerFactory // always set: real tsnet.Server or test mock
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

type servicesIdleTimeoutCmd struct{}

func (servicesIdleTimeoutCmd) svcCmd() {}

// ServicesServer manages a shared, ref-counted tsnet.Server for services mode.
// All mutable state is managed by a single event-loop goroutine.
type ServicesServer struct {
	log              zerolog.Logger
	cmds             chan servicesCmd
	done             chan struct{}
	hostname         string
	datadir          string
	authKey          string
	controlURL       string
	clientID         string
	clientSecret     string
	tags             string
	closed           atomic.Bool
	ephemeral        bool
	vipAPI           vipServiceAPI          // nil = production default
	listenerFactory  serviceListenerFactory // nil = production default
}

// NewServicesServer creates a ServicesServer and starts the event loop.
func NewServicesServer(cfg ServicesServerConfig) *ServicesServer {
	ss := &ServicesServer{
		hostname:        cfg.Hostname,
		datadir:         cfg.DataDir,
		authKey:         cfg.AuthKey,
		controlURL:      cfg.ControlURL,
		ephemeral:       cfg.Ephemeral,
		log:             cfg.Log.With().Str("services_server", cfg.Hostname).Logger(),
		cmds:            make(chan servicesCmd, 64), //nolint:mnd
		done:            make(chan struct{}),
		clientID:        cfg.ClientID,
		clientSecret:    cfg.ClientSecret,
		tags:            cfg.Tags,
		vipAPI:          cfg.VIPServiceAPI,
		listenerFactory: cfg.ListenerFactory,
	}
	go ss.loop()
	return ss
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
		case releaseServiceCmd:
			state, rt = ss.handleReleaseService(c, state, rt)
			if state == servicesIdle && rt != nil {
				idleTimer = time.AfterFunc(servicesIdleTimeout, func() {
					select {
					case ss.cmds <- servicesIdleTimeoutCmd{}:
					case <-ss.done:
					}
				})
			}
		case servicesIdleTimeoutCmd:
			if state == servicesIdle && rt != nil {
				ss.log.Info().Msg("services server idle timeout, stopping")
				ss.stopRuntime(rt)
				rt = nil
			}
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

	ss.log.Info().
		Str("service", c.serviceName).
		Uint16("port", c.port).
		Str("fqdn", listener.FQDN).
		Msg("service acquired")

	c.reply <- acquireServiceResult{listener, nil}
	return state, rt
}

func (ss *ServicesServer) handleReleaseService(c releaseServiceCmd, state servicesState, rt *servicesRuntime) (servicesState, *servicesRuntime) {
	if state == servicesRunning && rt != nil {
		key := serviceKey(c.serviceName, c.port)
		if entry, ok := rt.listeners[key]; ok {
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
		}

		c.reply <- nil

		if rt.refCount <= 0 {
			return servicesIdle, rt
		}
		return state, rt
	}
	c.reply <- nil
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

// Whois returns identity information. Services mode has limited WhoIs support.
func (ss *ServicesServer) Whois(_ *http.Request) model.Whois {
	// WhoIs is broken in ServiceModeHTTP (tailscale/tailscale#19215).
	return model.Whois{}
}

// startRuntime creates a new tsnet.Server and starts it.
func (ss *ServicesServer) startRuntime() *servicesRuntime {
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

	if err := tsServer.Start(); err != nil {
		ss.log.Error().Err(err).Msg("failed to start services tsnet server")
		cancel()
		return nil
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

	if rt.tsServer != nil {
		rt.tsServer.Close()
	}

	if rt.cancel != nil {
		rt.cancel()
	}
}

// newAPIClient creates a Tailscale API client with OAuth credentials.
func (ss *ServicesServer) newAPIClient() *tailscale.Client {
	return &tailscale.Client{
		Tailnet:   "-",
		UserAgent: userAgent,
		HTTP: tailscale.OAuthConfig{
			ClientID:     ss.clientID,
			ClientSecret: ss.clientSecret,
			Scopes:       []string{"services"},
		}.HTTPClient(),
	}
}

// createOrUpdateVIPService creates or updates a VIP Service via the Tailscale API.
func (ss *ServicesServer) createOrUpdateVIPService(serviceName string, ports []string) error {
	if api := ss.vipAPI; api != nil {
		return api.createOrUpdateVIPService(serviceName, ports)
	}
	return ss.createOrUpdateVIPServiceProd(serviceName, ports)
}

func (ss *ServicesServer) createOrUpdateVIPServiceProd(serviceName string, ports []string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second) //nolint:mnd
	defer cancel()

	client := ss.newAPIClient()

	return client.VIPServices().CreateOrUpdate(ctx, tailscale.VIPService{
		Name:  serviceName,
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

	client := ss.newAPIClient()

	return client.VIPServices().Delete(ctx, serviceName)
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

// cleanTags splits a comma-separated tag string and returns trimmed, non-empty tags.
func cleanTags(tags string) []string {
	parts := strings.Split(tags, ",")
	result := make([]string, 0, len(parts))
	for _, t := range parts {
		if t = strings.TrimSpace(t); t != "" {
			result = append(result, t)
		}
	}
	return result
}
