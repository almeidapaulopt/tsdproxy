// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

// Tests in this file exercise the OpenTelemetry instrumentation of the
// reverse-proxy port (upstream child spans, server-span attributes, W3C
// trace-context propagation). They are fully parallel-safe: each test builds
// its own TracerProvider AND propagator and passes the propagator explicitly
// into portProxyParams.Propagator (→ otelhttp.WithPropagators), so no global
// TextMapPropagator is ever touched.

package proxymanager

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	tracesdk "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	semconv "go.opentelemetry.io/otel/semconv/v1.41.0"
	"go.opentelemetry.io/otel/trace"

	"github.com/almeidapaulopt/tsdproxy/internal/core"
	"github.com/almeidapaulopt/tsdproxy/internal/model"
)

// newTestTracerProvider builds a TracerProvider with an in-memory sync exporter
// so tests can inspect the emitted spans without a real OTLP backend, plus the
// W3C TraceContext+Baggage propagator to pass explicitly into portProxyParams.
// It touches NO global state, so tests calling it are parallel-safe.
func newTestTracerProvider(t *testing.T) (*tracesdk.TracerProvider, propagation.TextMapPropagator, *tracetest.InMemoryExporter) {
	t.Helper()
	exporter := tracetest.NewInMemoryExporter()
	tp := tracesdk.NewTracerProvider(
		tracesdk.WithSyncer(exporter),
		tracesdk.WithResource(resource.NewSchemaless(
			semconv.ServiceName("tsdproxy-test"),
		)),
	)
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	return tp, core.NewPropagator(), exporter
}

// spanAttr reads a string attribute off a tracetest.SpanStub. Returns ok=false
// when absent.
func spanAttr(s tracetest.SpanStub, key string) (string, bool) {
	for _, kv := range s.Attributes {
		if string(kv.Key) == key {
			return kv.Value.AsString(), true
		}
	}
	return "", false
}

// TestPortProxy_EmitsServerAndUpstreamSpans is the headline telemetry test.
// It verifies that a single proxied request produces:
//  1. an inbound SERVER span, and
//  2. a child CLIENT span for the upstream container call, nested under it.
//
// Together these prove the reverse-proxy transport is instrumented, which is
// what lets a backend split "tsdproxy overhead" from "container latency".
//
// Note: otelhttp's default span-name formatter names the server span after the
// HTTP method (or matched route) rather than the NewHandler operation label,
// so we identify spans by SpanKind, not by name.
func TestPortProxy_EmitsServerAndUpstreamSpans(t *testing.T) {
	t.Parallel()

	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	backendURL, err := url.Parse(backend.URL)
	if err != nil {
		t.Fatalf("parse backend URL: %v", err)
	}

	pconfig := model.PortConfig{ProxyProtocol: "http", TLSValidate: false}
	pconfig.AddTarget(backendURL)

	tp, prop, exporter := newTestTracerProvider(t)

	p := newPortProxy(portProxyParams{
		Ctx:             context.Background(),
		PortConfig:      pconfig,
		Log:             zerolog.Nop(),
		WhoisMiddleware: func(next http.Handler) http.Handler { return next },
		ProxyName:       "my-proxy",
		PortName:        "web",
		TracerProvider:  tp,
		Propagator:      prop,
	})

	frontLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("frontend listen: %v", err)
	}
	go func() { _ = p.startWithListener(frontLn) }()
	defer p.close()

	resp, err := http.Get("http://" + frontLn.Addr().String() + "/")
	if err != nil {
		t.Fatalf("client GET: %v", err)
	}
	resp.Body.Close()

	serverSpan, upstreamSpan := findServerAndUpstream(t, exporter)

	// The upstream CLIENT span must be a child of the SERVER span: same trace,
	// upstream's parent == server's span ID.
	if serverSpan.SpanContext.SpanID() != upstreamSpan.Parent.SpanID() {
		t.Errorf("upstream span not child of server span: server=%q upstream.parent=%q",
			serverSpan.SpanContext.SpanID(), upstreamSpan.Parent.SpanID())
	}
	if serverSpan.SpanContext.TraceID() != upstreamSpan.SpanContext.TraceID() {
		t.Errorf("server and upstream spans belong to different traces: %q vs %q",
			serverSpan.SpanContext.TraceID(), upstreamSpan.SpanContext.TraceID())
	}
	if upstreamSpan.SpanKind != trace.SpanKindClient {
		t.Errorf("upstream span kind = %v, want Client", upstreamSpan.SpanKind)
	}
	if serverSpan.SpanKind != trace.SpanKindServer {
		t.Errorf("server span kind = %v, want Server", serverSpan.SpanKind)
	}
	// The upstream span uses our custom formatter.
	if want := "proxy upstream GET"; upstreamSpan.Name != want {
		t.Errorf("upstream span name = %q, want %q", upstreamSpan.Name, want)
	}
}

// findServerAndUpstream locates the SERVER span (inbound) and the CLIENT span
// (upstream container call) in the recorded span set. Fails the test if either
// is missing, since both are required for the telemetry contract to hold.
//
// It polls the exporter briefly because, although the sync exporter flushes on
// span End(), the http.Server's response path and the reverse-proxy's upstream
// client span can end in separate goroutines a hair after the client's
// resp.Body.Close() returns. A short bounded poll removes that timing flakiness
// without slowing the happy path.
func findServerAndUpstream(t *testing.T, exporter *tracetest.InMemoryExporter) (server, upstream *tracetest.SpanStub) {
	t.Helper()

	var spans []tracetest.SpanStub
	deadline := time.Now().Add(spanPollTimeout)
	for time.Now().Before(deadline) {
		spans = exporter.GetSpans()
		for i := range spans {
			switch spans[i].SpanKind {
			case trace.SpanKindServer:
				server = &spans[i]
			case trace.SpanKindClient:
				upstream = &spans[i]
			}
		}
		if server != nil && upstream != nil {
			return server, upstream
		}
		time.Sleep(time.Millisecond)
	}

	if server == nil {
		t.Fatalf("no SERVER span emitted (otelhttp handler not wired); recorded spans: %d", len(spans))
	}
	if upstream == nil {
		t.Fatalf("no CLIENT/upstream span emitted — reverse-proxy transport not instrumented; recorded spans: %d", len(spans))
	}
	return server, upstream
}

// spanPollTimeout bounds how long findServerAndUpstream waits for the sync
// exporter to surface both spans. Generous enough to absorb goroutine
// scheduling jitter, short enough to fail fast in CI.
const spanPollTimeout = 2 * time.Second

// TestPortProxy_ServerSpanHasProxyAttributes verifies the tsdproxy-specific
// attributes (proxy name, port, target, tailnet user) are attached to the
// inbound server span — the dimension parity that lets OpenObserve filter
// traces the same way Prometheus metrics are filtered.
func TestPortProxy_ServerSpanHasProxyAttributes(t *testing.T) {
	t.Parallel()

	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	backendURL, err := url.Parse(backend.URL)
	if err != nil {
		t.Fatalf("parse backend URL: %v", err)
	}

	pconfig := model.PortConfig{ProxyProtocol: "http", TLSValidate: false}
	pconfig.AddTarget(backendURL)

	tp, prop, exporter := newTestTracerProvider(t)

	whoisFunc := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := model.WhoisNewContext(r.Context(), model.Whois{
				ID:       "user-bob",
				Username: "bob",
			})
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}

	p := newPortProxy(portProxyParams{
		Ctx:             context.Background(),
		PortConfig:      pconfig,
		Log:             zerolog.Nop(),
		WhoisMiddleware: whoisFunc,
		ProxyName:       "attr-proxy",
		PortName:        "api",
		TracerProvider:  tp,
		Propagator:      prop,
	})

	frontLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("frontend listen: %v", err)
	}
	go func() { _ = p.startWithListener(frontLn) }()
	defer p.close()

	resp, err := http.Get("http://" + frontLn.Addr().String() + "/")
	if err != nil {
		t.Fatalf("client GET: %v", err)
	}
	resp.Body.Close()

	server, _ := findServerAndUpstream(t, exporter)

	checks := []struct{ key, want string }{
		{spanAttrProxyName, "attr-proxy"},
		{spanAttrPortName, "api"},
		{spanAttrTarget, backendURL.Host},
		{spanAttrTailnetUser, "user-bob"},
		{spanAttrTailnetLogin, "bob"},
	}
	for _, c := range checks {
		got, ok := spanAttr(*server, c.key)
		if !ok {
			t.Errorf("server span missing attribute %q", c.key)
			continue
		}
		if got != c.want {
			t.Errorf("server span attr %q = %q, want %q", c.key, got, c.want)
		}
	}
}

// TestPortProxy_TraceContextPropagatedToUpstream verifies the W3C traceparent
// header is forwarded to the container. This is the other half of upstream
// instrumentation: an instrumented backend (e.g. openobserve) will then emit a
// SERVER span nested under the tsdproxy proxy span instead of an orphan.
func TestPortProxy_TraceContextPropagatedToUpstream(t *testing.T) {
	t.Parallel()

	var (
		captured   http.Header
		capturedMu sync.Mutex
		gotRequest = make(chan struct{})
	)
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedMu.Lock()
		captured = r.Header.Clone()
		capturedMu.Unlock()
		close(gotRequest)
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	backendURL, err := url.Parse(backend.URL)
	if err != nil {
		t.Fatalf("parse backend URL: %v", err)
	}

	pconfig := model.PortConfig{ProxyProtocol: "http", TLSValidate: false}
	pconfig.AddTarget(backendURL)

	tp, prop, exporter := newTestTracerProvider(t)

	p := newPortProxy(portProxyParams{
		Ctx:             context.Background(),
		PortConfig:      pconfig,
		Log:             zerolog.Nop(),
		WhoisMiddleware: func(next http.Handler) http.Handler { return next },
		ProxyName:       "trace-proxy",
		PortName:        "web",
		TracerProvider:  tp,
		Propagator:      prop,
	})

	frontLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("frontend listen: %v", err)
	}
	go func() { _ = p.startWithListener(frontLn) }()
	defer p.close()

	resp, err := http.Get("http://" + frontLn.Addr().String() + "/")
	if err != nil {
		t.Fatalf("client GET: %v", err)
	}
	resp.Body.Close()

	<-gotRequest

	capturedMu.Lock()
	defer capturedMu.Unlock()
	tpHeader := captured.Get("Traceparent")
	if tpHeader == "" {
		t.Fatal("upstream did not receive a traceparent header — context not propagated")
	}

	// The propagated traceparent must carry the server span's trace ID so the
	// backend's span nests correctly.
	server, _ := findServerAndUpstream(t, exporter)
	wantTrace := server.SpanContext.TraceID().String()
	// traceparent format: 00-<traceId>-<spanId>-<flags>
	if len(tpHeader) < 36 || tpHeader[3:35] != wantTrace {
		t.Errorf("traceparent %q does not carry server trace ID %q", tpHeader, wantTrace)
	}
}

// TestPortProxy_5xxMarksServerErrorStatus verifies that an upstream 5xx flips
// the server span status to ERROR (the spec-compliant mapping), so failed
// backends are searchable in the trace backend.
func TestPortProxy_5xxMarksServerErrorStatus(t *testing.T) {
	t.Parallel()

	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer backend.Close()

	backendURL, err := url.Parse(backend.URL)
	if err != nil {
		t.Fatalf("parse backend URL: %v", err)
	}

	pconfig := model.PortConfig{ProxyProtocol: "http", TLSValidate: false}
	pconfig.AddTarget(backendURL)

	tp, prop, exporter := newTestTracerProvider(t)

	p := newPortProxy(portProxyParams{
		Ctx:             context.Background(),
		PortConfig:      pconfig,
		Log:             zerolog.Nop(),
		WhoisMiddleware: func(next http.Handler) http.Handler { return next },
		ProxyName:       "err-proxy",
		PortName:        "web",
		TracerProvider:  tp,
		Propagator:      prop,
	})

	frontLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("frontend listen: %v", err)
	}
	go func() { _ = p.startWithListener(frontLn) }()
	defer p.close()

	resp, err := http.Get("http://" + frontLn.Addr().String() + "/")
	if err != nil {
		t.Fatalf("client GET: %v", err)
	}
	resp.Body.Close()

	server, _ := findServerAndUpstream(t, exporter)
	if server.Status.Code != codes.Error {
		t.Errorf("server span status code = %v, want %v (Error) for 5xx upstream", server.Status.Code, codes.Error)
	}
}
