// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package proxymanager

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
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
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

const (
	maxIdleConnsPerHost = 10
	idleConnTimeout     = 30 * time.Second
	shutdownTimeout     = 10 * time.Second
	// defaultMaxHTTPConns bounds concurrent HTTP/HTTPS connections per port.
	// Higher than defaultMaxTCPConns because HTTP connections are typically
	// short-lived requests (and the dashboard's SSE stream counts as one
	// connection per viewer). The bound protects against slowloris-style
	// resource exhaustion from a single tailnet member.
	defaultMaxHTTPConns = 2048
	// maxHTTPHeaderBytes caps the total size of request headers accepted by
	// the reverse proxy. Matches Go stdlib's http.DefaultMaxHeaderBytes but
	// made explicit so reviewers don't have to know the implicit default.
	// Protects against memory-amplification via giant headers.
	maxHTTPHeaderBytes = 1 << 20 // 1 MiB
	canonicalLoopback  = "127.0.0.1"

	// Span attribute keys attached to proxy server spans. These are tsdproxy-
	// specific (not part of the OTel HTTP semantic conventions, which the
	// otelhttp handler already adds). They let dashboards filter/group traces
	// by the same dimensions Prometheus metrics already expose
	// (tsdproxy_proxy_*), plus the resolved tailnet user and the target
	// container — useful for attributing latency to a specific backend.
	spanAttrProxyName    = "tsdproxy.proxy.name"
	spanAttrPortName     = "tsdproxy.proxy.port"
	spanAttrTarget       = "tsdproxy.proxy.target"
	spanAttrTailnetUser  = "tsdproxy.user.id"
	spanAttrTailnetLogin = "tsdproxy.user.username"
)

type port struct {
	log         zerolog.Logger
	ctx         context.Context
	listener    net.Listener
	cancel      context.CancelFunc
	httpServer  *http.Server
	transport   *http.Transport
	limiter     *ipRateLimiter
	sem         chan struct{}
	activeConns atomic.Int64
	started     atomic.Bool
	mtx         sync.Mutex
}

// limitedListener wraps a net.Listener with a semaphore that bounds the number
// of concurrently-accepted connections. It is the HTTP-side analog of
// tcpPort.sem: a single tailnet client cannot exhaust the proxy by opening
// unlimited idle HTTP connections (slowloris-style resource amplification).
//
// Accept blocks until a slot is available, then returns a limitedConn that
// releases the slot on Close. Because http.Server.Serve calls Accept in a
// serial loop, blocking here naturally applies backpressure on new requests
// without rejecting in-flight ones — the dial completes at the kernel level
// but the request is not processed until a slot frees up.
//
// Close shuts the done channel so any Accept blocked on the semaphore unblocks
// immediately and returns net.ErrClosed — without this, http.Server.Shutdown
// would hang waiting for Serve to exit.
type limitedListener struct {
	net.Listener
	sem    chan struct{}
	active *atomic.Int64
	done   chan struct{}
	once   sync.Once
}

func (l *limitedListener) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	// Acquire AFTER Accept returns a real conn. Blocking here holds the
	// http.Server accept loop (serial), applying natural backpressure.
	// select on `done` so a Close while blocked on the semaphore wakes us
	// up — otherwise Shutdown would hang waiting for Serve to exit.
	select {
	case l.sem <- struct{}{}:
		if l.active != nil {
			l.active.Add(1)
		}
		return &limitedConn{Conn: c, sem: l.sem, active: l.active, once: &sync.Once{}}, nil
	case <-l.done:
		c.Close()
		return nil, net.ErrClosed
	}
}

func (l *limitedListener) Close() error {
	l.once.Do(func() { close(l.done) })
	return l.Listener.Close()
}

// limitedConn releases the semaphore slot exactly once when the connection is
// closed. http.Server closes connections for many reasons (idle timeout, shim
// teardown, client disconnect) so the release is guarded with sync.Once to
// avoid a double-release that would corrupt the semaphore counter.
type limitedConn struct {
	net.Conn
	sem    chan struct{}
	active *atomic.Int64
	once   *sync.Once
}

func (c *limitedConn) Close() error {
	c.once.Do(func() {
		<-c.sem
		if c.active != nil {
			c.active.Add(-1)
		}
	})
	return c.Conn.Close()
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
	httpPortStr string,
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
				if isManagementTarget(pconfig.GetFirstTarget(), httpPortStr) {
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

// spanAttrsMiddleware annotates the active server span with tsdproxy-specific
// attributes. It is wired INSIDE otelhttp.NewHandler so the span exists in the
// request context by the time this runs.
//
// Static attributes (proxy name, port name, target host:port) are set up-front
// — they are constant for the port's lifetime. The tailnet user (resolved by
// WhoisMiddleware) and the final HTTP status code are set after the inner
// handler returns. Status is mirrored onto the span as an attribute in addition
// to the otelhttp default http.response.status_code, so failed upstreams are
// searchable even when the server span itself stays UNSET (e.g. a 502 from the
// reverse-proxy ErrorHandler before any container response).
func spanAttrsMiddleware(proxyName, portName string, target *url.URL) func(http.Handler) http.Handler {
	staticAttrs := []attribute.KeyValue{
		attribute.String(spanAttrProxyName, proxyName),
		attribute.String(spanAttrPortName, portName),
	}
	if target != nil {
		staticAttrs = append(staticAttrs, attribute.String(spanAttrTarget, target.Host))
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Stamp static attrs on the server span before the inner handler
			// runs, so they appear even on early returns / panics.
			if span := trace.SpanFromContext(r.Context()); span.SpanContext().IsValid() {
				span.SetAttributes(staticAttrs...)

				// Tailnet identity was resolved by WhoisMiddleware (innermost)
				// and stashed in context. Attach it to the span so individual
				// user traffic can be isolated in the backend.
				if user, ok := model.WhoisFromContext(r.Context()); ok && user.ID != "" {
					span.SetAttributes(
						attribute.String(spanAttrTailnetUser, user.ID),
						attribute.String(spanAttrTailnetLogin, user.Username),
					)
				}
			}

			rec := &spanStatusRecorder{ResponseWriter: w, statusCode: http.StatusOK}
			next.ServeHTTP(rec, r)

			// Escalate 5xx to span status ERROR. otelhttp already sets
			// http.response.status_code and its own status mapping, but a 502
			// written by the reverse-proxy ErrorHandler (e.g. container down)
			// happens inside the inner handler chain, before otelhttp inspects
			// the response — mirroring the 5xx→ERROR mapping here guarantees
			// failed upstreams are flagged regardless of which layer wrote the
			// response.
			if rec.statusCode >= http.StatusInternalServerError {
				if span := trace.SpanFromContext(r.Context()); span.SpanContext().IsValid() {
					span.SetStatus(codes.Error, http.StatusText(rec.statusCode))
				}
			}
		})
	}
}

// spanStatusRecorder captures the response status code so spanAttrsMiddleware
// can attach it to the server span. It proxies Flush/Hijack/Unwrap to the
// underlying writer so SSE streaming (FlushInterval: -1) and connection
// upgrades keep working — the recorder sits innermost, directly wrapping the
// real ResponseWriter the reverse proxy flushes through.
type spanStatusRecorder struct {
	http.ResponseWriter
	statusCode int
	written    bool
}

func (r *spanStatusRecorder) WriteHeader(code int) {
	if !r.written {
		r.statusCode = code
		r.written = true
	}
	r.ResponseWriter.WriteHeader(code)
}

func (r *spanStatusRecorder) Write(b []byte) (int, error) {
	if !r.written {
		r.statusCode = http.StatusOK
		r.written = true
	}
	return r.ResponseWriter.Write(b)
}

func (r *spanStatusRecorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (r *spanStatusRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	h, ok := r.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, core.ErrHijackNotSupported
	}
	return h.Hijack()
}

func (r *spanStatusRecorder) Unwrap() http.ResponseWriter {
	return r.ResponseWriter
}

// portProxyParams holds all parameters for creating a new HTTP/HTTPS port proxy.
type portProxyParams struct {
	Log              zerolog.Logger
	Ctx              context.Context
	TracerProvider   trace.TracerProvider
	Propagator       propagation.TextMapPropagator
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
	// MaxHTTPConns bounds concurrent HTTP/HTTPS connections via a semaphore.
	// 0 means "use defaultMaxHTTPConns". Exposed for testability (small limit)
	// — mirrors newPortTCPWithLimit. See limitedListener.
	MaxHTTPConns int
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

	// Pre-compute the management HTTP port as a string. isManagementTarget
	// runs once per proxied request (proxyRewrite closure below); avoiding
	// strconv.FormatUint in the hot path saves an allocation per request.
	httpPortStr := strconv.FormatUint(uint64(p.HTTPPort), 10)

	// upstreamTransport wraps the bare *http.Transport in otelhttp so each
	// request forwarded to the container becomes a child CLIENT span of the
	// inbound server span. Two effects:
	//   1. Trace latency splits into "tsdproxy overhead" vs "container time".
	//   2. The W3C traceparent header is injected into the outbound request,
	//      so an instrumented upstream (e.g. openobserve) emits a SERVER span
	//      that nests under this proxy chain instead of starting an orphan.
	// Only applied when tracing is enabled — otherwise otelhttp would still
	// propagate context but create no-op spans (cheap, but pointless).
	var roundTripper http.RoundTripper = tr
	if p.TracerProvider != nil {
		transportOpts := []otelhttp.Option{
			otelhttp.WithTracerProvider(p.TracerProvider),
			// Name the upstream span after the configured target so it reads
			// e.g. "GET 127.0.0.1:8080" rather than the per-request rewritten
			// URL, which can be noisy for path-rich backends.
			otelhttp.WithSpanNameFormatter(func(_ string, r *http.Request) string {
				return "proxy upstream " + r.Method
			}),
		}
		if p.Propagator != nil {
			// Inject W3C traceparent explicitly so propagation does not depend
			// on the global TextMapPropagator.
			transportOpts = append(transportOpts, otelhttp.WithPropagators(p.Propagator))
		}
		roundTripper = otelhttp.NewTransport(tr, transportOpts...)
	}

	reverseProxy := &httputil.ReverseProxy{
		Transport:     roundTripper,
		FlushInterval: -1,
		ErrorHandler:  proxyErrorHandler(log, p.PortConfig),
		Rewrite:       proxyRewrite(p.PortConfig, p.IdentityHeaders, httpPortStr, p.ProxyAuthToken),
	}

	var limiter *ipRateLimiter
	if p.RateLimitEnabled {
		limiter = newIPRateLimiter(rate.Limit(p.RateLimitRPS), p.RateLimitBurst)
	}

	// Build the handler chain inside-out. spanAttrsMiddleware must sit INSIDE
	// WhoisMiddleware so the tailnet user is already in the request context by
	// the time it reads it, and INSIDE otelhttp.NewHandler so the server span
	// is active. The resulting flow (outer→inner):
	//   otelhttp → metrics → accessLog → rateLimit → whois → spanAttrs → reverseProxy
	var inner http.Handler = reverseProxy
	if p.TracerProvider != nil {
		inner = spanAttrsMiddleware(p.ProxyName, p.PortName, p.PortConfig.GetFirstTarget())(reverseProxy)
	}

	handler := p.WhoisMiddleware(inner)

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
		// otelhttp.NewHandler is the outermost wrapper: it creates the inbound
		// server span that every inner middleware annotates. Read/write events
		// give byte-level visibility for SSE/streaming bodies.
		handlerOpts := []otelhttp.Option{
			otelhttp.WithTracerProvider(p.TracerProvider),
			otelhttp.WithMessageEvents(otelhttp.ReadEvents, otelhttp.WriteEvents),
		}
		if p.Propagator != nil {
			handlerOpts = append(handlerOpts, otelhttp.WithPropagators(p.Propagator))
		}
		handler = otelhttp.NewHandler(handler, "proxy", handlerOpts...)
	}

	maxConns := p.MaxHTTPConns
	if maxConns <= 0 {
		maxConns = defaultMaxHTTPConns
	}

	httpServer := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: core.ReadHeaderTimeout,
		// MaxHeaderBytes explicit (matches Go stdlib default of 1 MiB) so
		// reviewers don't need to know the implicit default. Guards against
		// memory amplification via oversized request headers.
		MaxHeaderBytes: maxHTTPHeaderBytes,
		BaseContext:    func(net.Listener) context.Context { return ctxPort },
	}

	return &port{
		log:        log,
		ctx:        ctxPort,
		cancel:     cancel,
		httpServer: httpServer,
		transport:  tr,
		limiter:    limiter,
		sem:        make(chan struct{}, maxConns),
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
	if err := portStartLock(p.ctx, &p.mtx, &p.started); err != nil {
		l.Close()
		return fmt.Errorf("port closed before start: %w", err)
	}

	// p.sem is non-nil for proxy ports (bounded) and nil for redirect ports
	// (no cap needed — redirect handlers are O(1) per request).
	if p.sem != nil {
		l = &limitedListener{Listener: l, sem: p.sem, active: &p.activeConns, done: make(chan struct{})}
	}

	p.listener = l
	p.started.Store(true)
	p.mtx.Unlock()

	err := p.httpServer.Serve(l)

	// Always close the listener to prevent fd leaks. In the normal case,
	// http.Server.Shutdown already closed it (double-close returns
	// net.ErrClosed, which is harmless). In the start-vs-close race where
	// Shutdown ran before Serve registered the listener via trackListener,
	// this is the only close — preventing a permanent fd leak.
	_ = l.Close()

	defer p.log.Info().Msg("Terminating server")

	if err != nil && !errors.Is(err, net.ErrClosed) && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("error starting port: %w", err)
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

// isManagementTarget reports whether a proxy target points to tsdproxy's own
// management HTTP server (the self-proxy case where tsdproxy proxies to itself).
//
// Matches ONLY the canonical loopback address (127.0.0.1) plus the "localhost"
// alias, NOT the entire 127.0.0.0/8 range. A looser check (any ip.IsLoopback())
// would let a co-located service bound to e.g. 127.0.0.2:<same-port>/ receive
// the per-process auth token, which grants admin RBAC on the management API.
// The path check ensures only the root management server (no path prefix) is
// matched, preventing a co-located service at 127.0.0.1:<same-port>/some-path
// from receiving the token.
//
// httpPortStr is the pre-formatted decimal representation of the management
// port. It is pre-computed once per port in newPortProxy (R4 optimization) so
// the per-request comparison avoids a strconv.FormatUint allocation in the
// hot path.
func isManagementTarget(target *url.URL, httpPortStr string) bool {
	if target == nil {
		return false
	}
	host := target.Hostname()
	// Accept "localhost" alias and exact 127.0.0.1. Reject any other loopback
	// (e.g. 127.0.0.2) to prevent auth-token leakage via spoofed targets.
	if host != "localhost" && host != canonicalLoopback {
		return false
	}
	return target.Port() == httpPortStr &&
		(target.Path == "" || target.Path == "/")
}
