// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package proxymanager

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/almeidapaulopt/tsdproxy/internal/consts"
	"github.com/almeidapaulopt/tsdproxy/internal/model"
)

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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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

	require.Eventually(t, func() bool {
		conn, dialErr := net.Dial("tcp", ln.Addr().String())
		if dialErr != nil {
			return false
		}
		conn.Close()
		return true
	}, time.Second, 5*time.Millisecond)

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
	t.Parallel()
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
		nil,       // tracerProvider
		uint16(0), // httpPort
		"test-token",
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

	p := newPortProxy(context.Background(), pconfig, zerolog.Nop(), false, whoisFunc, nil, "test-proxy", "test-port", nil, false, nil, uint16(0), "test-token")

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
	t.Parallel()
	hdr := runPortProxyProtoTest(t, model.ProtoHTTPS)
	if got := hdr.Get("X-Forwarded-Proto"); got != "https" {
		t.Errorf("X-Forwarded-Proto: want https, got %q", got)
	}
}

func TestPortProxyForwardedProtoHTTP(t *testing.T) {
	t.Parallel()
	hdr := runPortProxyProtoTest(t, model.ProtoHTTP)
	if got := hdr.Get("X-Forwarded-Proto"); got != "http" {
		t.Errorf("X-Forwarded-Proto: want http, got %q", got)
	}
}

func TestPortProxyInjectsIdentityHeadersByDefault(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
		nil,   // tracerProvider
		uint16(0),
		"test-token",
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

func TestPortProxyStripsSpoofedXForwardedFor(t *testing.T) {
	t.Parallel()
	// Regression test for GHSA-pqg7-v6wh-3pfp: a client must not be able
	// to inject arbitrary X-Forwarded-For values that reach the upstream.
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

	whoisFunc := func(next http.Handler) http.Handler { return next }

	p := newPortProxy(context.Background(), pconfig, zerolog.Nop(), false, whoisFunc, nil, "test-proxy", "test-port", nil, false, nil, uint16(0), "test-token")

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
	req.Header.Add(consts.HeaderXForwardedFor, "1.2.3.4")
	req.Header.Add(consts.HeaderXForwardedFor, "5.6.7.8")
	req.Header.Set(consts.HeaderRealIP, "10.0.0.1")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("client GET: %v", err)
	}
	resp.Body.Close()

	capturedMu.Lock()
	defer capturedMu.Unlock()
	if captured == nil {
		t.Fatal("backend never received request")
	}

	xff := captured.Get(consts.HeaderXForwardedFor)
	if xff != "127.0.0.1" {
		t.Errorf("X-Forwarded-For: want 127.0.0.1, got %q", xff)
	}

	xRealIP := captured.Get(consts.HeaderRealIP)
	if xRealIP == "10.0.0.1" {
		t.Errorf("X-Real-IP: spoofed value leaked through: %q", xRealIP)
	}
}

func startUDPEchoBackend(t *testing.T) (net.Addr, *sync.WaitGroup) {
	t.Helper()
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen UDP: %v", err)
	}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		buf := make([]byte, 1024)
		for {
			n, addr, err := pc.ReadFrom(buf)
			if err != nil {
				return
			}
			_, _ = pc.WriteTo(buf[:n], addr)
		}
	}()
	t.Cleanup(func() {
		pc.Close()
		wg.Wait()
	})
	return pc.LocalAddr(), nil
}

func NewTestUDPConfig(t *testing.T, targetAddr string) model.PortConfig {
	t.Helper()
	targetURL, err := url.Parse("udp://" + targetAddr)
	if err != nil {
		t.Fatalf("failed to parse target URL: %v", err)
	}
	pconfig := model.PortConfig{
		ProxyProtocol: "udp",
	}
	pconfig.AddTarget(targetURL)
	return pconfig
}

func TestNewPortUDP(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	pconfig := model.PortConfig{ProxyProtocol: "udp"}
	targetURL, _ := url.Parse("udp://127.0.0.1:9999")
	pconfig.AddTarget(targetURL)

	up := newPortUDP(ctx, pconfig, zerolog.Nop())
	if up == nil {
		t.Fatal("newPortUDP returned nil")
	}
	if up.pconfig.ProxyProtocol != "udp" {
		t.Errorf("expected udp protocol, got %s", up.pconfig.ProxyProtocol)
	}
}

func TestUDPPort_StartWithListener_ReturnsError(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	pconfig := model.PortConfig{ProxyProtocol: "udp"}
	up := newPortUDP(ctx, pconfig, zerolog.Nop())

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	defer ln.Close()

	err = up.startWithListener(ln)
	if err == nil {
		t.Fatal("expected error when calling startWithListener on UDP port, got nil")
	}
}

func TestUDPPort_StartWithPacketConn_NoTarget(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	pconfig := model.PortConfig{ProxyProtocol: "udp"}
	up := newPortUDP(ctx, pconfig, zerolog.Nop())

	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen packet: %v", err)
	}
	defer pc.Close()

	err = up.startWithPacketConn(pc)
	if err == nil {
		t.Fatal("expected error for missing target, got nil")
	}
}

func TestUDPPort_EchoRelay(t *testing.T) {
	t.Parallel()
	backendAddr, _ := startUDPEchoBackend(t)

	ctx := context.Background()
	pconfig := NewTestUDPConfig(t, backendAddr.String())
	up := newPortUDP(ctx, pconfig, zerolog.Nop())

	frontPC, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to create frontend PacketConn: %v", err)
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- up.startWithPacketConn(frontPC)
	}()

	clientConn, err := net.DialUDP("udp", nil, frontPC.LocalAddr().(*net.UDPAddr))
	if err != nil {
		t.Fatalf("failed to dial frontend: %v", err)
	}
	defer clientConn.Close()

	msg := []byte("hello-udp")
	buf := make([]byte, 1024)

	require.Eventually(t, func() bool {
		if _, writeErr := clientConn.Write(msg); writeErr != nil {
			return false
		}
		_ = clientConn.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
		n, _, readErr := clientConn.ReadFrom(buf)
		if readErr != nil {
			return false
		}
		return string(buf[:n]) == "hello-udp"
	}, 2*time.Second, 10*time.Millisecond)

	up.close()
	<-errCh
}

func TestUDPPort_GetOrCreateBackendConn_Existing(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	backendAddr, _ := startUDPEchoBackend(t)
	pconfig := NewTestUDPConfig(t, backendAddr.String())
	up := newPortUDP(ctx, pconfig, zerolog.Nop())

	frontPC, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}

	clientMap := make(map[string]*clientEntry)
	var mapMtx sync.Mutex

	clientAddr := &net.UDPAddr{IP: net.ParseIP("10.0.0.1"), Port: 12345}

	// First call creates a new entry
	conn1, err := up.getOrCreateBackendConn(clientAddr, clientMap, &mapMtx, frontPC)
	if err != nil {
		t.Fatalf("first getOrCreateBackendConn failed: %v", err)
	}
	if conn1 == nil {
		t.Fatal("expected non-nil conn on first call")
	}
	if len(clientMap) != 1 {
		t.Fatalf("expected 1 client entry, got %d", len(clientMap))
	}

	// Second call returns existing entry
	conn2, err := up.getOrCreateBackendConn(clientAddr, clientMap, &mapMtx, frontPC)
	if err != nil {
		t.Fatalf("second getOrCreateBackendConn failed: %v", err)
	}
	if conn2 != conn1 {
		t.Fatal("expected same connection for existing client")
	}

	up.close()
	frontPC.Close()
}

func TestUDPPort_GetOrCreateBackendConn_MaxClientsEvicts(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	backendAddr, _ := startUDPEchoBackend(t)
	pconfig := NewTestUDPConfig(t, backendAddr.String())
	up := newPortUDP(ctx, pconfig, zerolog.Nop())

	frontPC, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}

	clientMap := make(map[string]*clientEntry)
	var mapMtx sync.Mutex

	// Fill to the max (minus 1 so one more triggers eviction)
	for i := 0; i < udpMaxClients-1; i++ {
		addr := &net.UDPAddr{IP: net.IPv4(10, 0, 0, byte(i/256)), Port: 30000 + i%65535}
		_, connErr := up.getOrCreateBackendConn(addr, clientMap, &mapMtx, frontPC)
		if connErr != nil {
			t.Fatalf("failed to create entry %d: %v", i, connErr)
		}
	}
	if len(clientMap) != udpMaxClients-1 {
		t.Fatalf("expected %d entries, got %d", udpMaxClients-1, len(clientMap))
	}

	// One more should succeed but evict the oldest
	newAddr := &net.UDPAddr{IP: net.ParseIP("10.0.0.99"), Port: 9999}
	_, err = up.getOrCreateBackendConn(newAddr, clientMap, &mapMtx, frontPC)
	if err != nil {
		t.Fatalf("failed to create entry after eviction: %v", err)
	}

	// Map must not exceed max capacity after eviction
	if len(clientMap) > udpMaxClients {
		t.Fatalf("expected at most %d entries after eviction, got %d", udpMaxClients, len(clientMap))
	}

	up.close()
	frontPC.Close()
}

func TestUDPPort_Close_Idempotent(_ *testing.T) {
	ctx := context.Background()
	pconfig := model.PortConfig{ProxyProtocol: "udp"}
	up := newPortUDP(ctx, pconfig, zerolog.Nop())

	up.close()
	up.close() // should not panic
}

func TestUDPPort_RelayBackendToClient_ClosesOnIdleTimeout(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	pconfig := model.PortConfig{ProxyProtocol: "udp"}
	up := newPortUDP(ctx, pconfig, zerolog.Nop())

	frontPC, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}

	clientMap := make(map[string]*clientEntry)
	var mapMtx sync.Mutex

	// Create a backend connection that never sends data — should timeout
	backendAddr, _ := startUDPEchoBackend(t)
	pconfig2 := NewTestUDPConfig(t, backendAddr.String())
	up.pconfig = pconfig2

	clientAddr := &net.UDPAddr{IP: net.ParseIP("10.0.0.1"), Port: 12345}
	conn, err := up.getOrCreateBackendConn(clientAddr, clientMap, &mapMtx, frontPC)
	if err != nil {
		t.Fatalf("getOrCreateBackendConn failed: %v", err)
	}
	if conn == nil {
		t.Fatal("expected non-nil conn")
	}

	up.close()
	frontPC.Close()
}

func TestEvictOldestClient(t *testing.T) {
	t.Parallel()
	clientMap := make(map[string]*clientEntry)

	now := time.Now()
	clientMap["old"] = &clientEntry{lastSeen: now.Add(-time.Hour)}
	clientMap["middle"] = &clientEntry{lastSeen: now.Add(-time.Minute)}
	clientMap["new"] = &clientEntry{lastSeen: now}

	evictOldestClient(clientMap)

	if _, exists := clientMap["old"]; exists {
		t.Error("expected oldest client to be evicted")
	}
	if _, exists := clientMap["middle"]; !exists {
		t.Error("expected middle client to remain")
	}
	if _, exists := clientMap["new"]; !exists {
		t.Error("expected newest client to remain")
	}
}

func TestEvictOldestClient_EmptyMap(_ *testing.T) {
	clientMap := make(map[string]*clientEntry)
	evictOldestClient(clientMap) // should not panic
}

func TestEvictOldestClient_SingleEntry(t *testing.T) {
	t.Parallel()
	clientMap := make(map[string]*clientEntry)
	clientMap["only"] = &clientEntry{lastSeen: time.Now()}
	evictOldestClient(clientMap)

	if len(clientMap) != 0 {
		t.Error("expected single entry to be evicted")
	}
}

func TestNewPortRedirect(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	pconfig := model.PortConfig{ProxyProtocol: "http"}
	targetURL, _ := url.Parse("https://example.com")
	pconfig.AddTarget(targetURL)

	rp := newPortRedirect(ctx, pconfig, zerolog.Nop())
	if rp == nil {
		t.Fatal("newPortRedirect returned nil")
	}
}

func TestHTTPRedirect_RedirectsToTarget(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	pconfig := model.PortConfig{ProxyProtocol: "http"}
	targetURL, _ := url.Parse("https://example.com/redirected")
	pconfig.AddTarget(targetURL)

	rp := newPortRedirect(ctx, pconfig, zerolog.Nop())

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- rp.startWithListener(ln)
	}()

	urlStr := "http://" + ln.Addr().String()

	// Do not follow redirects — we want the 301 response itself.
	client := &http.Client{
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := client.Get(urlStr)
	if err != nil {
		t.Fatalf("failed to GET: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusMovedPermanently {
		t.Fatalf("expected 301, got %d", resp.StatusCode)
	}

	loc := resp.Header.Get("Location")
	if loc != "https://example.com/redirected" {
		t.Fatalf("expected redirect to https://example.com/redirected, got %q", loc)
	}

	rp.close()
	<-errCh
}

func TestResolvePeerIP_DirectAddr(t *testing.T) {
	t.Parallel()
	r := &http.Request{
		RemoteAddr: "203.0.113.5:54321",
	}
	ip := resolvePeerIP(r)
	if ip != "203.0.113.5" {
		t.Fatalf("expected 203.0.113.5, got %q", ip)
	}
}

func TestResolvePeerIP_LocalhostWithSingleXFF(t *testing.T) {
	t.Parallel()
	r := &http.Request{
		RemoteAddr: "127.0.0.1:12345",
		Header:     make(http.Header),
	}
	r.Header.Set("X-Forwarded-For", "10.0.0.5")
	ip := resolvePeerIP(r)
	if ip != "10.0.0.5" {
		t.Fatalf("expected 10.0.0.5, got %q", ip)
	}
}

func TestResolvePeerIP_LocalhostNoXFF(t *testing.T) {
	t.Parallel()
	r := &http.Request{
		RemoteAddr: "127.0.0.1:12345",
	}
	ip := resolvePeerIP(r)
	if ip != "" {
		t.Fatalf("expected empty, got %q", ip)
	}
}

func TestResolvePeerIP_LocalhostWithMultipleXFF(t *testing.T) {
	t.Parallel()
	r := &http.Request{
		RemoteAddr: "127.0.0.1:12345",
		Header:     make(http.Header),
	}
	r.Header.Add("X-Forwarded-For", "10.0.0.5")
	r.Header.Add("X-Forwarded-For", "10.0.0.6")
	ip := resolvePeerIP(r)
	if ip != "" {
		t.Fatalf("expected empty for multiple XFF headers, got %q", ip)
	}
}

func TestResolvePeerIP_LocalhostWithCommaXFF(t *testing.T) {
	t.Parallel()
	r := &http.Request{
		RemoteAddr: "127.0.0.1:12345",
		Header:     make(http.Header),
	}
	r.Header.Set("X-Forwarded-For", "10.0.0.5, 10.0.0.6")
	ip := resolvePeerIP(r)
	if ip != "" {
		t.Fatalf("expected empty for comma-separated XFF, got %q", ip)
	}
}

func TestResolvePeerIP_LocalhostWithLoopbackXFF(t *testing.T) {
	t.Parallel()
	r := &http.Request{
		RemoteAddr: "127.0.0.1:12345",
		Header:     make(http.Header),
	}
	r.Header.Set("X-Forwarded-For", "127.0.0.1")
	ip := resolvePeerIP(r)
	if ip != "" {
		t.Fatalf("expected empty for loopback XFF, got %q", ip)
	}
}

func TestIsManagementTarget_Nil(t *testing.T) {
	if isManagementTarget(nil, uint16(8080)) {
		t.Error("expected false for nil target")
	}
}

func TestIsManagementTarget_NonLoopback(t *testing.T) {
	u, _ := url.Parse("https://example.com:8080")
	if isManagementTarget(u, uint16(8080)) {
		t.Error("expected false for non-loopback target")
	}
}

func TestIsManagementTarget_LocalhostLoopback(t *testing.T) {
	t.Parallel()
	u, _ := url.Parse("http://localhost:8080")
	if !isManagementTarget(u, uint16(8080)) {
		t.Error("expected true for localhost:8080 target")
	}
}

func TestIsManagementTarget_WrongPort(t *testing.T) {
	t.Parallel()
	u, _ := url.Parse("http://127.0.0.1:9090")
	if isManagementTarget(u, uint16(8080)) {
		t.Error("expected false for wrong port")
	}
}

func TestCloseAllClients(t *testing.T) {
	t.Parallel()
	clientMap := make(map[string]*clientEntry)
	var mapMtx sync.Mutex

	pc1, _ := net.ListenPacket("udp", "127.0.0.1:0")
	pc2, _ := net.ListenPacket("udp", "127.0.0.1:0")

	clientMap["a"] = &clientEntry{conn: pc1.(*net.UDPConn)}
	clientMap["b"] = &clientEntry{conn: pc2.(*net.UDPConn)}

	closeAllClients(clientMap, &mapMtx)

	if len(clientMap) != 2 {
		t.Fatalf("closeAllClients should not delete map entries, got %d", len(clientMap))
	}
}

func TestUDPPort_GetOrCreateBackendConn_NoTarget(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	pconfig := model.PortConfig{ProxyProtocol: "udp"}
	up := newPortUDP(ctx, pconfig, zerolog.Nop())

	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	defer pc.Close()

	clientMap := make(map[string]*clientEntry)
	var mapMtx sync.Mutex

	clientAddr := &net.UDPAddr{IP: net.ParseIP("10.0.0.1"), Port: 12345}
	_, err = up.getOrCreateBackendConn(clientAddr, clientMap, &mapMtx, pc)
	if err == nil {
		t.Fatal("expected error for missing target")
	}
}

func TestNewPortProxy_Minimal(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	pconfig := model.PortConfig{
		ProxyProtocol: "http",
	}
	targetURL, _ := url.Parse("http://backend:8080")
	pconfig.AddTarget(targetURL)

	p := newPortProxy(ctx, pconfig, zerolog.Nop(), false, func(next http.Handler) http.Handler {
		return next
	}, nil, "testproxy", "80", nil, false, nil, uint16(0), "test-token")
	if p == nil {
		t.Fatal("newPortProxy returned nil")
	}
	if p.httpServer == nil {
		t.Fatal("httpServer not initialized")
	}
}

func TestHTTPProxy_ContextCanceled_SilentNo502(t *testing.T) {
	t.Parallel()

	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(5 * time.Second)
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

	whoisFunc := func(next http.Handler) http.Handler { return next }

	p := newPortProxy(
		context.Background(), pconfig, zerolog.Nop(), false,
		whoisFunc, nil, "test-proxy", "test-port", nil, false,
		nil, uint16(0), "test-token",
	)

	frontLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("frontend listen: %v", err)
	}
	go func() { _ = p.startWithListener(frontLn) }()
	defer p.close()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+frontLn.Addr().String()+"/test", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		if !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
			t.Logf("request error (acceptable for canceled context): %v", err)
		}
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusBadGateway {
		t.Error("context.Canceled should NOT produce a 502 Bad Gateway")
	}
}

func TestHTTPRedirect_NilTarget_Returns502(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	pconfig := model.PortConfig{ProxyProtocol: "http"}

	rp := newPortRedirect(ctx, pconfig, zerolog.Nop())

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- rp.startWithListener(ln)
	}()

	resp, err := http.Get("http://" + ln.Addr().String() + "/")
	if err != nil {
		t.Fatalf("failed to GET: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("expected 502 for nil target, got %d", resp.StatusCode)
	}

	rp.close()
	<-errCh
}

func TestTCPPort_CancelledContext(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	pconfig := model.PortConfig{ProxyProtocol: "tcp"}
	tp := newPortTCP(ctx, pconfig, zerolog.Nop())

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to create frontend listener: %v", err)
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
