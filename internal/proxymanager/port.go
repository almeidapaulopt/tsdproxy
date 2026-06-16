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
	"net/url"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/time/rate"

	"github.com/almeidapaulopt/tsdproxy/internal/consts"
	"github.com/almeidapaulopt/tsdproxy/internal/core"
	"github.com/almeidapaulopt/tsdproxy/internal/core/metrics"
	"github.com/almeidapaulopt/tsdproxy/internal/model"

	"github.com/rs/zerolog"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/trace"
)

const (
	maxIdleConnsPerHost = 10
	idleConnTimeout     = 30 * time.Second
	tcpDialTimeout      = 10 * time.Second
	tcpErrChanBuf       = 2
	dialTimeout         = 10 * time.Second
	shutdownTimeout     = 10 * time.Second
	maxTCPAcceptRetries = 5
)

var errRateLimited = errors.New("UDP packet rate limited")

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
	limiter    *ipRateLimiter
	mtx        sync.Mutex
}

func proxyErrorHandler(log zerolog.Logger, pconfig model.PortConfig) func(w http.ResponseWriter, r *http.Request, err error) {
	return func(w http.ResponseWriter, r *http.Request, err error) {
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
	}
}

func proxyRewrite(
	pconfig model.PortConfig,
	identityHeaders bool,
	httpPort uint16,
	proxyAuthToken string,
) func(r *httputil.ProxyRequest) {
	return func(r *httputil.ProxyRequest) {
		target := pconfig.GetFirstTarget()
		if target != nil {
			r.SetURL(target)
		}
		r.Out.Host = r.In.Host

		// Always remove trusted identity headers to prevent spoofing.
		// Unauthenticated requests (e.g. Funnel) must not pass
		// attacker-controlled values through to the upstream.
		for _, h := range consts.TrustedProxyHeaders {
			r.Out.Header.Del(h)
		}

		// Inject authenticated user headers when enabled (default).
		// Some upstream services (e.g. wetty) consume Remote-User as the
		// SSH login username, which conflicts with their own auth flag —
		// the opt-out lets those services run without spurious overrides.
		if identityHeaders {
			if user, ok := model.WhoisFromContext(r.In.Context()); ok && user.ID != "" {
				r.Out.Header.Set(consts.HeaderID, user.ID)
				r.Out.Header.Set(consts.HeaderUsername, user.Username)
				r.Out.Header.Set(consts.HeaderDisplayName, user.DisplayName)
				r.Out.Header.Set(consts.HeaderProfilePicURL, user.ProfilePicURL)
				r.Out.Header.Set(consts.HeaderRemoteUser, user.Username)
				r.Out.Header.Set(consts.HeaderXForwardedUser, user.Username)
				r.Out.Header.Set(consts.HeaderXAuthRequestUser, user.Username)
				r.Out.Header.Set(consts.HeaderXForwardedEmail, user.Username)
				r.Out.Header.Set(consts.HeaderXAuthRequestEmail, user.Username)
				r.Out.Header.Set(consts.HeaderXForwardedPreferredUsername, user.DisplayName)

				// Forward the auth token only to the internal management
				// server (self-proxy case).  Never expose it to external
				// backends — a leaked token allows identity spoofing on
				// the management API.
				if isManagementTarget(pconfig.GetFirstTarget(), httpPort) {
					r.Out.Header.Set(consts.HeaderAuthToken, proxyAuthToken)
				}
			}
		}

		r.SetXForwarded()

		// SetXForwarded appends RemoteAddr to the outbound
		// X-Forwarded-For (stripped by TrustedProxyHeaders above).
		// Override with the single authoritative client IP to
		// prevent spoofing.
		if peerIP := resolvePeerIP(r.In); peerIP != "" {
			r.Out.Header.Set(consts.HeaderRealIP, peerIP)
			r.Out.Header.Set(consts.HeaderXForwardedFor, peerIP)
		}

		// Tailscale (and other TLS-terminating proxy providers) terminate TLS
		// before the request reaches this handler, so r.In.TLS is always nil.
		// Go's SetXForwarded incorrectly sets X-Forwarded-Proto to "http".
		// Override based on the port's configured proxy protocol so upstream
		// applications (e.g. Portainer CSRF) see the correct scheme.
		if pconfig.ProxyProtocol == model.ProtoHTTPS {
			r.Out.Header.Set("X-Forwarded-Proto", model.ProtoHTTPS)
		}
	}
}

// portProxyParams holds all parameters for creating a new HTTP/HTTPS port proxy.
type portProxyParams struct {
	Log              zerolog.Logger
	Ctx              context.Context
	TracerProvider   trace.TracerProvider
	Metrics          *metrics.Metrics
	WhoisMiddleware  func(next http.Handler) http.Handler
	LogBuffer        *LogRingBuffer
	ProxyName        string
	PortName         string
	ProxyAuthToken   string
	PortConfig       model.PortConfig
	RateLimitRPS     int
	RateLimitBurst   int
	HTTPPort         uint16
	AccessLog        bool
	IdentityHeaders  bool
	RateLimitEnabled bool
}

func newPortProxy(p portProxyParams) *port {
	log := p.Log.With().Str("port", p.PortConfig.String()).Logger()

	ctxPort, cancel := context.WithCancel(p.Ctx)

	if !p.PortConfig.TLSValidate {
		log.Warn().Str("port", p.PortConfig.String()).Msg("TLS validation disabled for this port")
	}
	tr := &http.Transport{
		TLSClientConfig:     &tls.Config{InsecureSkipVerify: !p.PortConfig.TLSValidate}, //nolint:gosec // G402: config-driven TLS validation toggle
		MaxIdleConnsPerHost: maxIdleConnsPerHost,
		IdleConnTimeout:     idleConnTimeout,
	}
	reverseProxy := &httputil.ReverseProxy{
		Transport:     tr,
		FlushInterval: -1,
		ErrorHandler:  proxyErrorHandler(log, p.PortConfig),
		Rewrite:       proxyRewrite(p.PortConfig, p.IdentityHeaders, p.HTTPPort, p.ProxyAuthToken),
	}

	var limiter *ipRateLimiter
	if p.RateLimitEnabled {
		limiter = newIPRateLimiter(rate.Limit(p.RateLimitRPS), p.RateLimitBurst)
	}

	handler := p.WhoisMiddleware(reverseProxy)

	if limiter != nil {
		handler = rateLimitMiddleware(limiter, handler)
	}

	if p.AccessLog {
		handler = core.LoggerMiddleware(log, handler, core.WithAccessLogWriter(p.LogBuffer))
	}

	if p.Metrics != nil {
		handler = p.Metrics.Middleware(p.ProxyName, p.PortName)(handler)
	}

	if p.TracerProvider != nil {
		handler = otelhttp.NewHandler(handler, "proxy",
			otelhttp.WithTracerProvider(p.TracerProvider),
			otelhttp.WithMessageEvents(otelhttp.ReadEvents, otelhttp.WriteEvents),
		)
	}

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
		limiter:    limiter,
	}
}

func newPortRedirect(ctx context.Context, pconfig model.PortConfig, log zerolog.Logger) *port {
	log = log.With().Str("port", pconfig.String()).Logger()

	ctxPort, cancel := context.WithCancel(ctx)

	redirectHTTPServer := &http.Server{
		ReadHeaderTimeout: core.ReadHeaderTimeout,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			target := pconfig.GetFirstTarget()
			if target == nil {
				http.Error(w, "no target configured", http.StatusBadGateway)
				return
			}
			http.Redirect(w, r, target.String(), http.StatusMovedPermanently)
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

	if p.limiter != nil {
		p.limiter.close()
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
	wg       sync.WaitGroup // track active connections
	acceptWg sync.WaitGroup // tracks whether startWithListener has finished
	started  atomic.Bool    // true once startWithListener has been called
}

func newPortTCP(ctx context.Context, pconfig model.PortConfig, log zerolog.Logger) *tcpPort {
	ctxPort, cancel := context.WithCancel(ctx)

	tp := &tcpPort{
		log:     log.With().Str("port", pconfig.String()).Logger(),
		ctx:     ctxPort,
		cancel:  cancel,
		pconfig: pconfig,
	}

	return tp
}

func (p *tcpPort) startWithListener(l net.Listener) error {
	p.acceptWg.Add(1)
	p.started.Store(true)
	defer p.acceptWg.Done()
	defer p.wg.Wait()

	p.mtx.Lock()
	p.listener = l
	p.mtx.Unlock()

	go func() {
		<-p.ctx.Done()
		l.Close()
	}()

	for retries := 0; ; retries++ {
		conn, err := l.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return nil
			}
			if retries < maxTCPAcceptRetries {
				p.log.Warn().Err(err).Int("retry", retries+1).Msg("transient TCP accept error, retrying")
				time.Sleep(time.Duration(retries+1) * time.Second)
				continue
			}
			p.log.Error().Err(err).Msg("error accepting TCP connection after retries")
			return fmt.Errorf("tcp accept: %w", err)
		}
		retries = 0

		p.wg.Add(1)
		go func(c net.Conn) {
			defer p.wg.Done()
			p.handleConn(c)
		}(conn)
	}
}

func (p *tcpPort) handleConn(clientConn net.Conn) {
	var backendConn net.Conn
	var clientCloseOnce, backendCloseOnce sync.Once

	closeClient := func() { clientCloseOnce.Do(func() { clientConn.Close() }) }
	closeBackend := func() { backendCloseOnce.Do(func() { backendConn.Close() }) }

	defer closeClient()

	target := p.pconfig.GetFirstTarget()
	if target == nil || target.Host == "" {
		p.log.Error().Msg("no target configured for TCP port")
		return
	}

	dialer := net.Dialer{Timeout: tcpDialTimeout}
	var err error
	backendConn, err = dialer.DialContext(p.ctx, "tcp", target.Host)
	if err != nil {
		p.log.Error().Err(err).Str("target", target.Host).Msg("error dialing backend")
		return
	}
	defer closeBackend()

	// Unblock io.Copy on shutdown: canceling p.ctx alone does not interrupt
	// an idle splice, so close both ends when the port is closed. Without this,
	// tcpPort.close()'s acceptWg.Wait() can hang indefinitely on idle
	// long-lived connections. sync.Once guards against double-close when
	// the shutdown goroutine and the normal exit path race.
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		select {
		case <-p.ctx.Done():
			closeClient()
			closeBackend()
		case <-stop:
		}
	}()

	errChan := make(chan error, tcpErrChanBuf)
	go func() {
		_, err := io.Copy(backendConn, clientConn)
		errChan <- err
	}()
	go func() {
		_, err := io.Copy(clientConn, backendConn)
		errChan <- err
	}()

	<-errChan
	closeClient()
	closeBackend()
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

	if p.started.Load() {
		p.acceptWg.Wait()
	}

	return errs
}

// udpPort forwards UDP packets from the Tailscale PacketConn to the target backend.
type udpPort struct {
	log          zerolog.Logger
	ctx          context.Context
	conn         net.PacketConn
	cancel       context.CancelFunc
	clientMap    map[string]*clientEntry
	pconfig      model.PortConfig
	wg           sync.WaitGroup
	mtx          sync.Mutex
	clientMapMtx sync.Mutex
	started      atomic.Bool
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
	target := p.pconfig.GetFirstTarget()
	if target == nil || target.Host == "" {
		p.started.Store(true)
		return errors.New("no target configured for UDP port")
	}

	if err := p.ctx.Err(); err != nil {
		p.started.Store(true)
		pc.Close()
		return fmt.Errorf("udp port closed before start: %w", err)
	}

	// Add(1) MUST precede started.Store(true) so that close() — which gates
	// wg.Wait() on started.Load() — can never observe started==true while the
	// WaitGroup counter is still 0. Storing first opens a window where close()
	// returns without waiting for this goroutine (leaking the relay + conn) and
	// trips the race detector's "WaitGroup misuse" check. Mirrors tcpPort.
	p.wg.Add(1)
	p.started.Store(true)
	defer p.wg.Done()

	p.mtx.Lock()
	p.conn = pc
	p.clientMap = make(map[string]*clientEntry)
	p.mtx.Unlock()

	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		<-p.ctx.Done()
		pc.Close()
	}()

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
	conn      *net.UDPConn
	lastSeen  time.Time
	limiter   *rate.Limiter
	closeOnce sync.Once
}

func (e *clientEntry) close() {
	if e.conn != nil {
		e.closeOnce.Do(func() {
			e.conn.Close()
		})
	}
}

func (p *udpPort) relayPackets(pc net.PacketConn) {
	buf := make([]byte, udpBufSize)

	defer closeAllClients(p.clientMap, &p.clientMapMtx)

	for {
		n, clientAddr, err := pc.ReadFrom(buf)
		if err != nil {
			if errors.Is(err, net.ErrClosed) || p.ctx.Err() != nil {
				return
			}
			p.log.Error().Err(err).Msg("error reading UDP packet")
			return
		}

		backend, err := p.getOrCreateBackendConn(clientAddr, p.clientMap, &p.clientMapMtx, pc)
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
		entry.close()
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
	if target == nil || target.Host == "" {
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

	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		p.relayBackendToClient(entry, pc, clientAddr, mapMtx, clientMap)
	}()

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
		oldest.close()
		delete(clientMap, oldestKey)
	}
}

func (p *udpPort) relayBackendToClient(entry *clientEntry, pc net.PacketConn, clientAddr net.Addr, mapMtx *sync.Mutex, clientMap map[string]*clientEntry) {
	backend := entry.conn
	defer func() {
		entry.close()
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
	clientMap := p.clientMap
	clientMapMtx := &p.clientMapMtx
	p.mtx.Unlock()

	if clientMap != nil {
		closeAllClients(clientMap, clientMapMtx)
	}

	p.cancel()

	if p.started.Load() {
		p.wg.Wait()
	}

	return errs
}

// isManagementTarget reports whether a proxy target points to tsdproxy's own
// management HTTP server (the self-proxy case where tsdproxy proxies to itself).
//
// Heuristic: loopback IP + management port + empty/root path. This accepts any
// loopback address, not just the address the server actually binds to. The path
// check ensures only the root management server (no path prefix) is matched,
// preventing a co-located service at 127.0.0.1:<same-port>/some-path from
// receiving the per-process auth token.
func isManagementTarget(target *url.URL, httpPort uint16) bool {
	if target == nil {
		return false
	}
	host := target.Hostname()
	// net.ParseIP does not recognize "localhost"; map to the numeric form so
	// that list-provider targets like "http://localhost:8080" are detected.
	if host == "localhost" {
		host = "127.0.0.1"
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback() &&
		target.Port() == strconv.FormatUint(uint64(httpPort), 10) &&
		(target.Path == "" || target.Path == "/")
}
