// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package proxymanager

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"os"
	"sync"
	"time"

	"golang.org/x/time/rate"

	"github.com/almeidapaulopt/tsdproxy/internal/config"
	"github.com/almeidapaulopt/tsdproxy/internal/consts"
	"github.com/almeidapaulopt/tsdproxy/internal/core"
	"github.com/almeidapaulopt/tsdproxy/internal/core/metrics"
	"github.com/almeidapaulopt/tsdproxy/internal/model"

	"github.com/rs/zerolog"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

var errRateLimited = errors.New("UDP packet rate limited")

const (
	dialTimeout     = 10 * time.Second
	shutdownTimeout = 10 * time.Second
)

// portHandler is the interface implemented by all port types (HTTP proxy, HTTP redirect, TCP forward).
type portHandler interface {
	startWithListener(net.Listener) error
	close() error
}

type port struct {
	log        zerolog.Logger
	ctx        context.Context
	listener   net.Listener
	cancel     context.CancelFunc
	httpServer *http.Server
	transport  *http.Transport
	mtx        sync.Mutex
}

func newPortProxy(
	ctx context.Context,
	pconfig model.PortConfig,
	log zerolog.Logger,
	accessLog bool,
	whoisFunc func(next http.Handler) http.Handler,
	m *metrics.Metrics,
	proxyName string,
	portName string,
	logBuffer *LogRingBuffer,
	identityHeaders bool,
) *port {
	//
	log = log.With().Str("port", pconfig.String()).Logger()

	ctxPort, cancel := context.WithCancel(ctx)

	// Create the reverse proxy
	//
	tr := &http.Transport{
		TLSClientConfig:     &tls.Config{InsecureSkipVerify: !pconfig.TLSValidate}, //nolint
		MaxIdleConnsPerHost: 10,                                                    //nolint:mnd
		IdleConnTimeout:     30 * time.Second,                                      //nolint:mnd
	}
	reverseProxy := &httputil.ReverseProxy{
		Transport:     tr,
		FlushInterval: -1, // flush immediately — required for SSE streaming
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			// When the client disconnects (typical for SSE/long-lived
			// connections) the request context is canceled. Don't write
			// a 502 in that case — there is nobody to read it, and
			// EventSource clients would otherwise interpret the body
			// as a real backend failure. Aborting silently lets the
			// browser's EventSource auto-reconnect.
			if errors.Is(err, context.Canceled) ||
				errors.Is(r.Context().Err(), context.Canceled) {
				log.Debug().
					Str("port", pconfig.String()).
					Str("url", r.URL.String()).
					Msg("client closed connection")
				return
			}
			log.Error().Err(err).
				Str("port", pconfig.String()).
				Str("url", r.URL.String()).
				Msg("proxy error")
			w.WriteHeader(http.StatusBadGateway)
		},
		Rewrite: func(r *httputil.ProxyRequest) {
			r.SetURL(pconfig.GetFirstTarget())
			r.Out.Host = r.In.Host

			// Always remove trusted identity headers to prevent spoofing.
			// Unauthenticated requests (e.g. Funnel) must not pass
			// attacker-controlled values through to the upstream.
			r.Out.Header.Del(consts.HeaderID)
			r.Out.Header.Del(consts.HeaderUsername)
			r.Out.Header.Del(consts.HeaderDisplayName)
			r.Out.Header.Del(consts.HeaderProfilePicURL)
			r.Out.Header.Del(consts.HeaderAuthToken)
			r.Out.Header.Del(consts.HeaderRemoteUser)
			r.Out.Header.Del(consts.HeaderXForwardedUser)
			r.Out.Header.Del(consts.HeaderXAuthRequestUser)
			r.Out.Header.Del(consts.HeaderXForwardedEmail)
			r.Out.Header.Del(consts.HeaderXAuthRequestEmail)
			r.Out.Header.Del(consts.HeaderXForwardedPreferredUsername)

			// Inject authenticated user headers when enabled (default).
			// Some upstream services (e.g. wetty) consume Remote-User as the
			// SSH login username, which conflicts with their own auth flag —
			// the opt-out lets those services run without spurious overrides.
			if identityHeaders {
				if user, ok := model.WhoisFromContext(r.In.Context()); ok {
					r.Out.Header.Set(consts.HeaderID, user.ID)
					r.Out.Header.Set(consts.HeaderUsername, user.Username)
					r.Out.Header.Set(consts.HeaderDisplayName, user.DisplayName)
					r.Out.Header.Set(consts.HeaderProfilePicURL, user.ProfilePicURL)
					r.Out.Header.Set(consts.HeaderAuthToken, core.ProxyAuthToken())
					r.Out.Header.Set(consts.HeaderRemoteUser, user.Username)
					r.Out.Header.Set(consts.HeaderXForwardedUser, user.Username)
					r.Out.Header.Set(consts.HeaderXAuthRequestUser, user.Username)
					r.Out.Header.Set(consts.HeaderXForwardedEmail, user.Username)
					r.Out.Header.Set(consts.HeaderXAuthRequestEmail, user.Username)
					r.Out.Header.Set(consts.HeaderXForwardedPreferredUsername, user.DisplayName)
				}
			}

			r.SetXForwarded()
		},
	}

	handler := whoisFunc(reverseProxy)

	if accessLog {
		handler = core.LoggerMiddleware(log, handler, core.WithAccessLogWriter(logBuffer))
	}

	// add metrics as outermost middleware
	if m != nil {
		handler = m.Middleware(proxyName, portName)(handler)
	}

	if config.Config.Telemetry.Enabled {
		handler = otelhttp.NewHandler(handler, "proxy", otelhttp.WithMessageEvents(otelhttp.ReadEvents, otelhttp.WriteEvents))
	}

	// main http Server
	httpServer := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: core.ReadHeaderTimeout,
		BaseContext:       func(net.Listener) context.Context { return ctxPort },
	}

	return &port{
		log:        log,
		ctx:        ctxPort,
		cancel:     cancel,
		httpServer: httpServer,
		transport:  tr,
	}
}

func newPortRedirect(ctx context.Context, pconfig model.PortConfig, log zerolog.Logger) *port {
	log = log.With().Str("port", pconfig.String()).Logger()

	ctxPort, cancel := context.WithCancel(ctx)

	redirectHTTPServer := &http.Server{
		ReadHeaderTimeout: core.ReadHeaderTimeout,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, pconfig.GetFirstTarget().String(), http.StatusMovedPermanently)
		}),
	}

	return &port{
		log:        log,
		ctx:        ctxPort,
		cancel:     cancel,
		httpServer: redirectHTTPServer,
	}
}

func (p *port) startWithListener(l net.Listener) error {
	p.mtx.Lock()
	p.listener = l
	p.mtx.Unlock()

	err := p.httpServer.Serve(l)
	defer p.log.Info().Msg("Terminating server")

	if err != nil && !errors.Is(err, net.ErrClosed) && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("error starting port %w", err)
	}
	return nil
}

func (p *port) close() error {
	var errs error

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer shutdownCancel()

	if p.httpServer != nil {
		errs = errors.Join(errs, p.httpServer.Shutdown(shutdownCtx))
	}

	// http.Server.Shutdown closes the listener; skip explicit Close() to
	// avoid "use of closed network connection" double-close errors.

	if p.transport != nil {
		p.transport.CloseIdleConnections()
	}

	p.cancel()

	return errs
}

// tcpPort forwards raw TCP connections from the Tailscale listener to the target backend.
type tcpPort struct {
	log      zerolog.Logger
	ctx      context.Context
	listener net.Listener
	cancel   context.CancelFunc
	pconfig  model.PortConfig
	mtx      sync.Mutex
}

func newPortTCP(ctx context.Context, pconfig model.PortConfig, log zerolog.Logger) *tcpPort {
	ctxPort, cancel := context.WithCancel(ctx)

	return &tcpPort{
		log:     log.With().Str("port", pconfig.String()).Logger(),
		ctx:     ctxPort,
		cancel:  cancel,
		pconfig: pconfig,
	}
}

func (p *tcpPort) startWithListener(l net.Listener) error {
	p.mtx.Lock()
	p.listener = l
	p.mtx.Unlock()

	go func() {
		<-p.ctx.Done()
		l.Close()
	}()

	for {
		conn, err := l.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return nil
			}
			p.log.Error().Err(err).Msg("error accepting TCP connection")
			return fmt.Errorf("tcp accept: %w", err)
		}

		go p.handleConn(conn)
	}
}

func (p *tcpPort) handleConn(clientConn net.Conn) {
	defer clientConn.Close()

	target := p.pconfig.GetFirstTarget()
	if target.Host == "" {
		p.log.Error().Msg("no target configured for TCP port")
		return
	}

	dialer := net.Dialer{Timeout: 10 * time.Second} //nolint:mnd
	backendConn, err := dialer.DialContext(p.ctx, "tcp", target.Host)
	if err != nil {
		p.log.Error().Err(err).Str("target", target.Host).Msg("error dialing backend")
		return
	}
	defer backendConn.Close()

	errChan := make(chan error, 2) //nolint:mnd
	go func() {
		_, err := io.Copy(backendConn, clientConn)
		errChan <- err
	}()
	go func() {
		_, err := io.Copy(clientConn, backendConn)
		errChan <- err
	}()

	<-errChan
	<-errChan
}

func (p *tcpPort) close() error {
	p.mtx.Lock()
	var errs error
	if p.listener != nil {
		errs = errors.Join(errs, p.listener.Close())
	}
	p.mtx.Unlock()

	p.cancel()

	return errs
}

// udpPort forwards UDP packets from the Tailscale PacketConn to the target backend.
type udpPort struct {
	log     zerolog.Logger
	ctx     context.Context
	conn    net.PacketConn
	cancel  context.CancelFunc
	pconfig model.PortConfig
	mtx     sync.Mutex
}

func newPortUDP(ctx context.Context, pconfig model.PortConfig, log zerolog.Logger) *udpPort {
	ctxPort, cancel := context.WithCancel(ctx)

	return &udpPort{
		log:     log.With().Str("port", pconfig.String()).Logger(),
		ctx:     ctxPort,
		cancel:  cancel,
		pconfig: pconfig,
	}
}

func (p *udpPort) startWithListener(_ net.Listener) error {
	return errors.New("UDP ports must use startWithPacketConn, not startWithListener")
}

func (p *udpPort) startWithPacketConn(pc net.PacketConn) error {
	p.mtx.Lock()
	p.conn = pc
	p.mtx.Unlock()

	go func() {
		<-p.ctx.Done()
		pc.Close()
	}()

	target := p.pconfig.GetFirstTarget()
	if target.Host == "" {
		return errors.New("no target configured for UDP port")
	}

	p.relayPackets(pc)
	return nil
}

const (
	udpBufSize        = 64 * 1024
	udpIdleTimeout    = 2 * time.Minute
	udpMaxClients     = 1024
	udpPerSourceRate  = 500
	udpPerSourceBurst = 1000
)

// clientEntry tracks a per-client backend UDP connection and its last activity.
type clientEntry struct {
	conn     *net.UDPConn
	lastSeen time.Time
	limiter  *rate.Limiter
}

func (p *udpPort) relayPackets(pc net.PacketConn) {
	buf := make([]byte, udpBufSize)

	clientMap := make(map[string]*clientEntry)
	var mapMtx sync.Mutex

	defer closeAllClients(clientMap, &mapMtx)

	for {
		n, clientAddr, err := pc.ReadFrom(buf)
		if err != nil {
			if errors.Is(err, net.ErrClosed) || p.ctx.Err() != nil {
				return
			}
			p.log.Error().Err(err).Msg("error reading UDP packet")
			return
		}

		backend, err := p.getOrCreateBackendConn(clientAddr, clientMap, &mapMtx, pc)
		if err != nil {
			if errors.Is(err, errRateLimited) {
				continue
			}
			p.log.Error().Err(err).Str("client", clientAddr.String()).Msg("error dialing backend")
			continue
		}

		if backend == nil {
			continue
		}

		if _, err := backend.Write(buf[:n]); err != nil {
			if errors.Is(err, net.ErrClosed) {
				continue
			}
			p.log.Error().Err(err).Msg("error writing to backend")
		}
	}
}

func closeAllClients(clientMap map[string]*clientEntry, mapMtx *sync.Mutex) {
	mapMtx.Lock()
	for _, entry := range clientMap {
		if entry.conn != nil {
			entry.conn.Close()
		}
	}
	mapMtx.Unlock()
}

// getOrCreateBackendConn returns an existing or new backend UDP connection for the client.
// Caller must NOT hold mapMtx.
func (p *udpPort) getOrCreateBackendConn(
	clientAddr net.Addr,
	clientMap map[string]*clientEntry,
	mapMtx *sync.Mutex,
	pc net.PacketConn,
) (*net.UDPConn, error) {
	//
	mapMtx.Lock()
	defer mapMtx.Unlock()

	key := clientAddr.String()
	if entry, ok := clientMap[key]; ok {
		entry.lastSeen = time.Now()
		if !entry.limiter.Allow() {
			p.log.Debug().Str("client", clientAddr.String()).Msg("UDP packet rate limited")
			return nil, errRateLimited
		}
		return entry.conn, nil
	}

	entry := &clientEntry{
		lastSeen: time.Now(),
		limiter:  rate.NewLimiter(udpPerSourceRate, udpPerSourceBurst),
	}
	clientMap[key] = entry

	if !entry.limiter.Allow() {
		p.log.Debug().Str("client", clientAddr.String()).Msg("UDP packet rate limited")
		return nil, errRateLimited
	}

	if len(clientMap) >= udpMaxClients {
		evictOldestClient(clientMap)
	}

	// Resolve target for each new client connection so re-resolution takes effect.
	target := p.pconfig.GetFirstTarget()
	if target.Host == "" {
		delete(clientMap, key)
		return nil, errors.New("no target configured for UDP port")
	}

	backendAddr, err := net.ResolveUDPAddr(model.ProtoUDP, target.Host)
	if err != nil {
		delete(clientMap, key)
		return nil, fmt.Errorf("error resolving backend UDP address: %w", err)
	}

	conn, err := net.DialUDP(model.ProtoUDP, nil, backendAddr)
	if err != nil {
		delete(clientMap, key)
		return nil, err
	}

	entry.conn = conn

	go p.relayBackendToClient(entry, pc, clientAddr, mapMtx, clientMap)

	return conn, nil
}

func evictOldestClient(clientMap map[string]*clientEntry) {
	var oldestKey string
	var oldestTime time.Time
	for k, v := range clientMap {
		if oldestKey == "" || v.lastSeen.Before(oldestTime) {
			oldestKey = k
			oldestTime = v.lastSeen
		}
	}
	if oldest, ok := clientMap[oldestKey]; ok {
		if oldest.conn != nil {
			oldest.conn.Close()
		}
		delete(clientMap, oldestKey)
	}
}

func (p *udpPort) relayBackendToClient(entry *clientEntry, pc net.PacketConn, clientAddr net.Addr, mapMtx *sync.Mutex, clientMap map[string]*clientEntry) {
	backend := entry.conn
	defer func() {
		backend.Close()
		mapMtx.Lock()
		// Delete only if this entry hasn't been replaced (e.g. by eviction + re-creation).
		if current, ok := clientMap[clientAddr.String()]; ok && current == entry {
			delete(clientMap, clientAddr.String())
		}
		mapMtx.Unlock()
	}()

	buf := make([]byte, udpBufSize)
	for {
		if err := backend.SetReadDeadline(time.Now().Add(udpIdleTimeout)); err != nil {
			return
		}

		n, err := backend.Read(buf)
		if err != nil {
			// Idle timeout — client mapping expires gracefully.
			if errors.Is(err, os.ErrDeadlineExceeded) {
				return
			}
			// Conn closed (shutdown or eviction).
			if errors.Is(err, net.ErrClosed) || p.ctx.Err() != nil {
				return
			}
			p.log.Error().Err(err).Msg("error reading from backend")
			return
		}

		if _, err := pc.WriteTo(buf[:n], clientAddr); err != nil {
			return
		}
	}
}

func (p *udpPort) close() error {
	p.mtx.Lock()
	var errs error
	if p.conn != nil {
		errs = errors.Join(errs, p.conn.Close())
	}
	p.mtx.Unlock()

	p.cancel()

	return errs
}
