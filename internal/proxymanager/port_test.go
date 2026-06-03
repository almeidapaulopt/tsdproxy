// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package proxymanager

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"github.com/almeidapaulopt/tsdproxy/internal/config"
	"github.com/almeidapaulopt/tsdproxy/internal/consts"
	"github.com/almeidapaulopt/tsdproxy/internal/model"
)

func TestMain(m *testing.M) {
	config.SetTestConfig(os.TempDir(), "tskey-test")
	os.Exit(m.Run())
}

func newTestTCPConfig(t *testing.T, targetAddr string) model.PortConfig {
	t.Helper()
	targetURL, err := url.Parse("tcp://" + targetAddr)
	if err != nil {
		t.Fatalf("failed to parse target URL: %v", err)
	}
	pconfig := model.PortConfig{
		ProxyProtocol: "tcp",
	}
	pconfig.AddTarget(targetURL)
	return pconfig
}

func startEchoBackend(t *testing.T) (net.Listener, *sync.WaitGroup) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				_, _ = io.Copy(c, c)
			}(conn)
		}
	}()
	return ln, &wg
}

func TestTCPPortForward(t *testing.T) {
	backendLn, backendWg := startEchoBackend(t)
	defer backendLn.Close()

	pconfig := newTestTCPConfig(t, backendLn.Addr().String())
	tp := newPortTCP(context.Background(), pconfig, zerolog.Nop())

	frontLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to create frontend listener: %v", err)
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- tp.startWithListener(frontLn)
	}()

	clientConn, err := net.DialTimeout("tcp", frontLn.Addr().String(), 2*time.Second)
	if err != nil {
		t.Fatalf("failed to connect to frontend: %v", err)
	}
	defer clientConn.Close()

	message := []byte("hello tcp proxy")
	if _, writeErr := clientConn.Write(message); writeErr != nil {
		t.Fatalf("failed to write: %v", writeErr)
	}

	buf := make([]byte, len(message))
	if dlErr := clientConn.SetReadDeadline(time.Now().Add(2 * time.Second)); dlErr != nil {
		t.Fatalf("failed to set deadline: %v", dlErr)
	}
	n, err := clientConn.Read(buf)
	if err != nil {
		t.Fatalf("failed to read: %v", err)
	}
	if string(buf[:n]) != string(message) {
		t.Fatalf("expected %q, got %q", message, buf[:n])
	}

	if closeErr := clientConn.Close(); closeErr != nil {
		t.Fatalf("failed to close client: %v", closeErr)
	}

	tp.close()
	backendLn.Close()
	backendWg.Wait()

	if err := <-errCh; err != nil {
		t.Fatalf("startWithListener returned error: %v", err)
	}
}

func TestTCPPortMultipleConnections(t *testing.T) {
	backendLn, backendWg := startEchoBackend(t)
	defer backendLn.Close()

	pconfig := newTestTCPConfig(t, backendLn.Addr().String())
	tp := newPortTCP(context.Background(), pconfig, zerolog.Nop())

	frontLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to create frontend listener: %v", err)
	}

	go func() { _ = tp.startWithListener(frontLn) }()

	for i := range 5 {
		clientConn, err := net.DialTimeout("tcp", frontLn.Addr().String(), 2*time.Second)
		if err != nil {
			t.Fatalf("connection %d: failed to connect: %v", i, err)
		}

		message := []byte("message " + string(rune('A'+i)))
		if _, writeErr := clientConn.Write(message); writeErr != nil {
			t.Fatalf("connection %d: failed to write: %v", i, writeErr)
		}

		buf := make([]byte, len(message))
		if dlErr := clientConn.SetReadDeadline(time.Now().Add(2 * time.Second)); dlErr != nil {
			t.Fatalf("connection %d: failed to set deadline: %v", i, dlErr)
		}
		n, err := clientConn.Read(buf)
		if err != nil {
			t.Fatalf("connection %d: failed to read: %v", i, err)
		}
		if string(buf[:n]) != string(message) {
			t.Fatalf("connection %d: expected %q, got %q", i, message, buf[:n])
		}
		clientConn.Close()
	}

	tp.close()
	backendLn.Close()
	backendWg.Wait()
}

func TestTCPPortClose(t *testing.T) {
	pconfig := model.PortConfig{ProxyProtocol: "tcp"}
	tp := newPortTCP(context.Background(), pconfig, zerolog.Nop())

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- tp.startWithListener(ln)
	}()

	time.Sleep(50 * time.Millisecond)

	if closeErr := tp.close(); closeErr != nil {
		t.Fatalf("close returned error: %v", closeErr)
	}

	select {
	case startErr := <-errCh:
		if startErr != nil {
			t.Fatalf("startWithListener returned error after close: %v", startErr)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("startWithListener did not return after close")
	}

	_, err = ln.Accept()
	if err == nil {
		t.Fatal("expected error from Accept on closed listener")
	}
}

func TestTCPPortEmptyTarget(t *testing.T) {
	pconfig := model.PortConfig{ProxyProtocol: "tcp"}
	tp := newPortTCP(context.Background(), pconfig, zerolog.Nop())

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}

	go func() { _ = tp.startWithListener(ln) }()

	clientConn, err := net.DialTimeout("tcp", ln.Addr().String(), 2*time.Second)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer clientConn.Close()

	if dlErr := clientConn.SetReadDeadline(time.Now().Add(500 * time.Millisecond)); dlErr != nil {
		t.Fatalf("failed to set deadline: %v", dlErr)
	}
	buf := make([]byte, 1024)
	n, err := clientConn.Read(buf)

	if n != 0 || err == nil {
		t.Fatal("expected connection to be closed by server when no target configured")
	}

	tp.close()
	ln.Close()
}

// runPortProxyHeaderTest spins up an echo backend that captures incoming
// headers, points a newPortProxy at it, and returns the headers seen by the
// upstream after a single request.
//
// identityHeaders controls whether identity header injection is enabled.
func runPortProxyHeaderTest(t *testing.T, identityHeaders bool) http.Header {
	t.Helper()

	var captured http.Header
	var capturedMu sync.Mutex
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedMu.Lock()
		captured = r.Header.Clone()
		capturedMu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	backendURL, err := url.Parse(backend.URL)
	if err != nil {
		t.Fatalf("parse backend URL: %v", err)
	}

	pconfig := model.PortConfig{
		ProxyProtocol: "http",
		TLSValidate:   false,
	}
	pconfig.AddTarget(backendURL)

	whoisFunc := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := model.WhoisNewContext(r.Context(), model.Whois{
				ID:            "user-alice",
				Username:      "alice",
				DisplayName:   "Alice",
				ProfilePicURL: "https://example.com/alice.png",
			})
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}

	p := newPortProxy(
		context.Background(),
		pconfig,
		zerolog.Nop(),
		false, // accessLog
		whoisFunc,
		nil, // metrics
		"test-proxy",
		"test-port",
		nil, // logBuffer
		identityHeaders,
	)

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

	capturedMu.Lock()
	defer capturedMu.Unlock()
	if captured == nil {
		t.Fatal("backend never received request")
	}
	return captured
}

func runPortProxyProtoTest(t *testing.T, proxyProtocol string) http.Header {
	t.Helper()

	var captured http.Header
	var capturedMu sync.Mutex
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedMu.Lock()
		captured = r.Header.Clone()
		capturedMu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	backendURL, err := url.Parse(backend.URL)
	if err != nil {
		t.Fatalf("parse backend URL: %v", err)
	}

	pconfig := model.PortConfig{
		ProxyProtocol: proxyProtocol,
		TLSValidate:   false,
	}
	pconfig.AddTarget(backendURL)

	whoisFunc := func(next http.Handler) http.Handler { return next }

	p := newPortProxy(
		context.Background(),
		pconfig,
		zerolog.Nop(),
		false,
		whoisFunc,
		nil,
		"test-proxy",
		"test-port",
		nil,
		false,
	)

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

	capturedMu.Lock()
	defer capturedMu.Unlock()
	if captured == nil {
		t.Fatal("backend never received request")
	}
	return captured
}

func TestPortProxyForwardedProtoHTTPS(t *testing.T) {
	hdr := runPortProxyProtoTest(t, model.ProtoHTTPS)
	if got := hdr.Get("X-Forwarded-Proto"); got != "https" {
		t.Errorf("X-Forwarded-Proto: want https, got %q", got)
	}
}

func TestPortProxyForwardedProtoHTTP(t *testing.T) {
	hdr := runPortProxyProtoTest(t, model.ProtoHTTP)
	if got := hdr.Get("X-Forwarded-Proto"); got != "http" {
		t.Errorf("X-Forwarded-Proto: want http, got %q", got)
	}
}

func TestPortProxyInjectsIdentityHeadersByDefault(t *testing.T) {
	hdr := runPortProxyHeaderTest(t, true)

	if got := hdr.Get(consts.HeaderRemoteUser); got != "alice" {
		t.Errorf("Remote-User: want alice, got %q", got)
	}
	if got := hdr.Get(consts.HeaderXForwardedUser); got != "alice" {
		t.Errorf("X-Forwarded-User: want alice, got %q", got)
	}
	if got := hdr.Get(consts.HeaderUsername); got != "alice" {
		t.Errorf("X-TSDProxy-Username: want alice, got %q", got)
	}
	if got := hdr.Get(consts.HeaderDisplayName); got != "Alice" {
		t.Errorf("X-TSDProxy-Displayname: want Alice, got %q", got)
	}
}

func TestPortProxyOmitsIdentityHeadersWhenDisabled(t *testing.T) {
	hdr := runPortProxyHeaderTest(t, false)

	// All identity headers must be absent — even though Whois succeeded.
	for _, name := range []string{
		consts.HeaderRemoteUser,
		consts.HeaderXForwardedUser,
		consts.HeaderXAuthRequestUser,
		consts.HeaderXForwardedEmail,
		consts.HeaderXAuthRequestEmail,
		consts.HeaderXForwardedPreferredUsername,
		consts.HeaderUsername,
		consts.HeaderDisplayName,
		consts.HeaderProfilePicURL,
	} {
		if got := hdr.Get(name); got != "" {
			t.Errorf("%s: want empty, got %q", name, got)
		}
	}
}

func TestPortProxyAlwaysStripsClientIdentityHeaders(t *testing.T) {
	// Regression: even with IdentityHeaders=false, a client-supplied
	// identity header must never reach the upstream (anti-spoofing).
	var captured http.Header
	var capturedMu sync.Mutex
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedMu.Lock()
		captured = r.Header.Clone()
		capturedMu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	backendURL, err := url.Parse(backend.URL)
	if err != nil {
		t.Fatalf("parse backend URL: %v", err)
	}

	pconfig := model.PortConfig{ProxyProtocol: "http"}
	pconfig.AddTarget(backendURL)

	// whoisFunc that does NOT inject a user — so the only source of
	// identity headers would be the client.
	whoisFunc := func(next http.Handler) http.Handler { return next }

	p := newPortProxy(
		context.Background(),
		pconfig,
		zerolog.Nop(),
		false,
		whoisFunc,
		nil,
		"test-proxy",
		"test-port",
		nil,
		false, // opted out — strip block must still run
	)

	frontLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("frontend listen: %v", err)
	}
	go func() { _ = p.startWithListener(frontLn) }()
	defer p.close()

	req, err := http.NewRequest(http.MethodGet, "http://"+frontLn.Addr().String()+"/", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set(consts.HeaderRemoteUser, "spoofed")
	req.Header.Set(consts.HeaderXForwardedUser, "spoofed")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("client GET: %v", err)
	}
	resp.Body.Close()

	capturedMu.Lock()
	defer capturedMu.Unlock()
	if got := captured.Get(consts.HeaderRemoteUser); got != "" {
		t.Errorf("Remote-User leaked: %q (must be stripped)", got)
	}
	if got := captured.Get(consts.HeaderXForwardedUser); got != "" {
		t.Errorf("X-Forwarded-User leaked: %q (must be stripped)", got)
	}
}

func TestTCPPortCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	pconfig := model.PortConfig{ProxyProtocol: "tcp"}
	tp := newPortTCP(ctx, pconfig, zerolog.Nop())

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- tp.startWithListener(ln)
	}()

	cancel()

	select {
	case <-errCh:
	case <-time.After(2 * time.Second):
		t.Fatal("startWithListener did not return after context cancellation")
	}

	ln.Close()
}
