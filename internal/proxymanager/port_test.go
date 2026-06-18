// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package proxymanager

import (
	"context"
	"errors"
	"fmt"
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
	tp := newPortTCP(context.Background(), pconfig, zerolog.Nop(), nil, "", "")

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
	tp := newPortTCP(context.Background(), pconfig, zerolog.Nop(), nil, "", "")

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
	tp := newPortTCP(context.Background(), pconfig, zerolog.Nop(), nil, "", "")

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
	tp := newPortTCP(context.Background(), pconfig, zerolog.Nop(), nil, "", "")

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

	p := newPortProxy(portProxyParams{
		Ctx:              context.Background(),
		PortConfig:       pconfig,
		Log:              zerolog.Nop(),
		WhoisMiddleware:  whoisFunc,
		ProxyName:        "test-proxy",
		PortName:         "test-port",
		IdentityHeaders:  identityHeaders,
		RateLimitEnabled: true,
		RateLimitRPS:     100,
		RateLimitBurst:   200,
		ProxyAuthToken:   "test-token",
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

	p := newPortProxy(portProxyParams{
		Ctx:              context.Background(),
		PortConfig:       pconfig,
		Log:              zerolog.Nop(),
		WhoisMiddleware:  whoisFunc,
		ProxyName:        "test-proxy",
		PortName:         "test-port",
		RateLimitEnabled: true,
		RateLimitRPS:     100,
		RateLimitBurst:   200,
		ProxyAuthToken:   "test-token",
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

	p := newPortProxy(portProxyParams{
		Ctx:              context.Background(),
		PortConfig:       pconfig,
		Log:              zerolog.Nop(),
		WhoisMiddleware:  whoisFunc,
		ProxyName:        "test-proxy",
		PortName:         "test-port",
		IdentityHeaders:  false, // opted out — strip block must still run
		RateLimitEnabled: true,
		RateLimitRPS:     100,
		RateLimitBurst:   200,
		ProxyAuthToken:   "test-token",
	})

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

	p := newPortProxy(portProxyParams{
		Ctx:              context.Background(),
		PortConfig:       pconfig,
		Log:              zerolog.Nop(),
		WhoisMiddleware:  whoisFunc,
		ProxyName:        "test-proxy",
		PortName:         "test-port",
		RateLimitEnabled: true,
		RateLimitRPS:     100,
		RateLimitBurst:   200,
		ProxyAuthToken:   "test-token",
	})

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

	up := newPortUDP(ctx, pconfig, zerolog.Nop(), nil, "", "")
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
	up := newPortUDP(ctx, pconfig, zerolog.Nop(), nil, "", "")

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
	up := newPortUDP(ctx, pconfig, zerolog.Nop(), nil, "", "")
	defer up.close()

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
	up := newPortUDP(ctx, pconfig, zerolog.Nop(), nil, "", "")

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
	up := newPortUDP(ctx, pconfig, zerolog.Nop(), nil, "", "")

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

	closeAllClients(clientMap, &mapMtx)
	up.close()
	frontPC.Close()
}

func TestUDPPort_GetOrCreateBackendConn_MaxClientsEvicts(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	backendAddr, _ := startUDPEchoBackend(t)
	pconfig := NewTestUDPConfig(t, backendAddr.String())
	up := newPortUDP(ctx, pconfig, zerolog.Nop(), nil, "", "")

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

	closeAllClients(clientMap, &mapMtx)
	up.close()
	frontPC.Close()
}

func TestUDPPort_Close_Idempotent(_ *testing.T) {
	ctx := context.Background()
	pconfig := model.PortConfig{ProxyProtocol: "udp"}
	up := newPortUDP(ctx, pconfig, zerolog.Nop(), nil, "", "")

	up.close()
	up.close() // should not panic
}

func TestUDPPort_RelayBackendToClient_ClosesOnIdleTimeout(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	pconfig := model.PortConfig{ProxyProtocol: "udp"}
	up := newPortUDP(ctx, pconfig, zerolog.Nop(), nil, "", "")

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

	closeAllClients(clientMap, &mapMtx)
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
	up := newPortUDP(ctx, pconfig, zerolog.Nop(), nil, "", "")

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

	p := newPortProxy(portProxyParams{
		Ctx:              ctx,
		PortConfig:       pconfig,
		Log:              zerolog.Nop(),
		WhoisMiddleware:  func(next http.Handler) http.Handler { return next },
		ProxyName:        "testproxy",
		PortName:         "80",
		RateLimitEnabled: true,
		RateLimitRPS:     100,
		RateLimitBurst:   200,
		ProxyAuthToken:   "test-token",
	})
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

	p := newPortProxy(portProxyParams{
		Ctx:              context.Background(),
		PortConfig:       pconfig,
		Log:              zerolog.Nop(),
		WhoisMiddleware:  whoisFunc,
		ProxyName:        "test-proxy",
		PortName:         "test-port",
		RateLimitEnabled: true,
		RateLimitRPS:     100,
		RateLimitBurst:   200,
		ProxyAuthToken:   "test-token",
	})

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
	tp := newPortTCP(ctx, pconfig, zerolog.Nop(), nil, "", "")

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

// ---------------------------------------------------------------------------
// Bug-demonstrating tests — these SHOULD FAIL with the current code.
// ---------------------------------------------------------------------------

// TestUDPPort_CloseBeforeStart_GoroutineStompsConn demonstrates BUG-1:
// udpPort.close() can return before startWithPacketConn has been scheduled,
// because the WaitGroup counter is 0 at that point (wg.Add(1) hasn't been
// called yet). The goroutine then runs AFTER close() returned, stomping
// p.conn and p.clientMap with fresh values over already-closed state.
//
// tcpPort already solved this with a `started atomic.Bool` guard that
// prevents wg.Wait() from being called on a zero counter. udpPort is
// missing the same guard.
//
// This test calls close() first, then startWithPacketConn, to deterministically
// simulate the race where Proxy.Close() fires before the UDP goroutine gets
// scheduled (Proxy.Start launches `go udp.startWithPacketConn(pc)` at
// proxy.go:851, and startListeners runs outside opMu so Close can race).
//
// After the fix, startWithPacketConn should detect the already-canceled
// context and NOT set p.conn.
func TestUDPPort_CloseBeforeStart_GoroutineStompsConn(t *testing.T) {
	t.Parallel()

	backendAddr, _ := startUDPEchoBackend(t)
	pconfig := NewTestUDPConfig(t, backendAddr.String())
	up := newPortUDP(context.Background(), pconfig, zerolog.Nop(), nil, "", "")

	frontPC, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to create frontend PacketConn: %v", err)
	}
	t.Cleanup(func() { frontPC.Close() })

	// Close BEFORE startWithPacketConn — deterministically simulates the race
	// where Proxy.Close() fires before the UDP goroutine gets scheduled.
	// close() calls p.cancel() and p.wg.Wait(). Since the goroutine hasn't
	// done wg.Add(1) yet, Wait() returns immediately (counter=0).
	up.close()

	// Now launch startWithPacketConn. With the bug, this runs even though
	// close() has already returned, violating the WaitGroup contract
	// ("Add with positive delta at counter=0 must happen before Wait").
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = up.startWithPacketConn(frontPC)
	}()

	select {
	case <-done:
		// The goroutine completed — it ran AFTER close() returned.
	case <-time.After(5 * time.Second):
		t.Fatal("startWithPacketConn hung — possible deadlock")
	}

	// With the bug: the goroutine set p.conn to frontPC (stomping the nil
	// left by close()). With the fix: startWithPacketConn should detect
	// the already-canceled context and return early without setting p.conn.
	up.mtx.Lock()
	connAfterClose := up.conn
	up.mtx.Unlock()

	if connAfterClose != nil {
		t.Error("BUG: p.conn was set to non-nil after close() returned — " +
			"startWithPacketConn ran untracked, stomping closed state. " +
			"Missing `started` atomic.Bool guard (see tcpPort pattern at port.go:386)")
	}
}

// ---------------------------------------------------------------------------
// Additional bug-reproduction tests for the security/bug review.
// Each test asserts the CORRECT (post-fix) behavior, so it FAILS today and
// passes once the bug is fixed. Per AGENTS.md bug-fix TDD protocol.
// ---------------------------------------------------------------------------

// TestIsManagementTarget_RejectsNonLocalhostLoopback_BUG reproduces H-1:
// isManagementTarget uses ip.IsLoopback() which is true for the entire
// 127.0.0.0/8 range (e.g. 127.0.0.2, 127.5.6.7). The per-process auth token
// (which grants admin RBAC on the management API) is forwarded to ANY target
// matching loopback + port + root path, not just the exact bind address.
//
// Attack scenario:
//  1. Attacker has list-provider config write access OR runs a container in
//     host-network mode that binds to 127.0.0.2:<mgmt-port>/.
//  2. They configure target: http://127.0.0.2:8080/ in tsdproxy.yaml or labels.
//  3. isManagementTarget returns true → auth token injected into the request
//     to the attacker's listener → full admin API compromise.
//
// Expected post-fix: isManagementTarget should match ONLY the exact IP the
// management server binds to (typically 127.0.0.1), not any loopback address.
//
// Today: the 127.0.0.2 and 127.1.2.3 subtests fail (returns true, want false).
func TestIsManagementTarget_RejectsNonLocalhostLoopback_BUG(t *testing.T) {
	t.Parallel()

	const mgmtPort uint16 = 8080

	cases := []struct {
		name string
		host string
		want bool
	}{
		{name: "exact loopback", host: "127.0.0.1", want: true},
		{name: "localhost alias", host: "localhost", want: true},
		{name: "non-localhost loopback 127.0.0.2", host: "127.0.0.2", want: false},
		{name: "non-localhost loopback 127.1.2.3", host: "127.1.2.3", want: false},
		{name: "non-localhost loopback 127.255.255.254", host: "127.255.255.254", want: false},
		{name: "private non-loopback", host: "192.168.1.1", want: false},
		{name: "link-local metadata endpoint", host: "169.254.169.254", want: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			u := &url.URL{
				Scheme: "http",
				Host:   fmt.Sprintf("%s:%d", tc.host, mgmtPort),
				Path:   "/",
			}
			got := isManagementTarget(u, mgmtPort)
			require.Equal(t, tc.want, got,
				"isManagementTarget(%s) = %v, want %v — "+
					"BUG: accepts any 127.x.x.x loopback, enabling auth-token leak via "+
					"loopback-spoofed management target (port.go:766-780)", tc.host, got, tc.want)
		})
	}
}

// TestTCPPort_HasNoConnectionLimit_BUG reproduces H-3: tcpPort accepts
// unlimited concurrent connections. UDP is bounded at 1024 clients
// (port.go udpMaxClients), HTTP IPs at 4096 (ratelimit.go httpRateLimitClients),
// but TCP has no equivalent cap.
//
// Attack scenario: any tailnet member opens 50k idle TCP connections →
// ~150k goroutines → memory exhaustion → tsdproxy OOM-killed → ALL proxies
// drop, not just the targeted one.
//
// Expected post-fix: a `maxTCPConcurrent` semaphore (e.g. 1024, matching UDP)
// should reject connections beyond the limit.
//
// Today: this test fails because all 5 connections succeed (no limit).
func TestTCPPort_HasNoConnectionLimit_BUG(t *testing.T) {
	t.Parallel()

	backendLn, backendWg := startEchoBackend(t)
	t.Cleanup(func() {
		// Close THEN wait (correct order; defer LIFO would deadlock).
		backendLn.Close()
		backendWg.Wait()
	})

	pconfig := newTestTCPConfig(t, backendLn.Addr().String())
	const testMaxConns = 3
	tp := newPortTCPWithLimit(context.Background(), pconfig, zerolog.Nop(), nil, "", "", testMaxConns)

	frontLn, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	conns := make([]net.Conn, 0, 5)
	t.Cleanup(func() {
		// Close client conns first so handleConn's io.Copy unblocks and
		// tp.close()'s acceptWg.Wait() returns promptly.
		for _, c := range conns {
			_ = c.Close()
		}
		_ = tp.close()
		_ = frontLn.Close()
	})

	go func() { _ = tp.startWithListener(frontLn) }()

	// Open more than testMaxConns connections. After the fix, the semaphore
	// rejects the overflow at Accept time. Before the fix, all are accepted.
	const numConns = 5
	for i := 0; i < numConns; i++ {
		c, dialErr := net.DialTimeout("tcp", frontLn.Addr().String(), 500*time.Millisecond)
		if dialErr != nil {
			continue
		}
		conns = append(conns, c)
	}

	// Give the server time to process all pending accepts. TCP handshakes
	// complete in the kernel before the server's userspace Accept returns,
	// so client-side dial success does NOT reflect server-side acceptance.
	// We measure server-side activeConns instead.
	require.Eventually(t, func() bool {
		return tp.activeConns.Load() <= int64(testMaxConns)
	}, 500*time.Millisecond, 10*time.Millisecond,
		"BUG: tcpPort has %d active connections — semaphore should cap at %d. "+
			"UDP is bounded at udpMaxClients=1024 and HTTP at httpRateLimitClients=4096; "+
			"TCP now has maxTCPConns (default %d, test override %d).",
		tp.activeConns.Load(), testMaxConns, defaultMaxTCPConns, testMaxConns)
}

// flakyAcceptListener wraps a net.Listener and returns transient errors for

// TestHTTPPort_StartWithListenerAfterClose_ReturnsErrServerClosed is a
// NEGATIVE confirmation: the originally suspected bug ("startWithListener
// hangs forever after close()") does NOT exist. Go's http.Server.trackListener
// mechanism sets s.shutdown=true during Shutdown(), which causes subsequent
// Serve() calls to return ErrServerClosed immediately.
//
// This test documents the WORKING behavior so future contributors don't
// re-attempt the same "fix". It PASSES today.
func TestHTTPPort_StartWithListenerAfterClose_ReturnsErrServerClosed(t *testing.T) {
	t.Parallel()

	pconfig := model.PortConfig{ProxyProtocol: "http"}
	targetURL, _ := url.Parse("http://127.0.0.1:1")
	pconfig.AddTarget(targetURL)

	p := newPortProxy(portProxyParams{
		Ctx:             context.Background(),
		PortConfig:      pconfig,
		Log:             zerolog.Nop(),
		WhoisMiddleware: func(next http.Handler) http.Handler { return next },
		ProxyName:       "test",
		PortName:        "p",
	})

	// Close BEFORE startWithListener. http.Server.Shutdown sets s.shutdown=true.
	require.NoError(t, p.close())

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer ln.Close()

	done := make(chan error, 1)
	go func() {
		done <- p.startWithListener(ln)
	}()

	select {
	case err := <-done:
		// Go's stdlib returns ErrServerClosed via trackListener, so Serve
		// never blocks. startWithListener returns nil (the error is filtered
		// at port.go:272).
		require.NoError(t, err, "expected nil — ErrServerClosed is filtered")
	case <-time.After(2 * time.Second):
		t.Fatal("startWithListener should return promptly after close() — " +
			"http.Server.trackListener returns ErrServerClosed when shutdown flag is set")
	}
}

// flakyAcceptListener wraps a net.Listener and returns transient errors for
// the first N Accept calls, then delegates to the underlying listener.
// Used by TestTCPPort_AcceptRetrySleepNotCtxAware_BUG to trigger the
// time.Sleep retry path in tcpPort.startWithListener (port.go:358-361).
type flakyAcceptListener struct {
	net.Listener
	failsRemaining int
	mu             sync.Mutex
}

func (l *flakyAcceptListener) Accept() (net.Conn, error) {
	l.mu.Lock()
	if l.failsRemaining > 0 {
		l.failsRemaining--
		l.mu.Unlock()
		return nil, errors.New("flaky transient error")
	}
	l.mu.Unlock()
	return l.Listener.Accept()
}

// TestTCPPort_AcceptRetrySleepNotCtxAware_BUG reproduces the shutdown-delay
// bug at port.go:360. The TCP accept retry loop uses time.Sleep without
// consulting p.ctx — on shutdown during a transient Accept error, close()
// blocks for up to 15 seconds (1+2+3+4+5s for retries 0-4) before noticing
// the listener was closed.
//
// Expected post-fix: the sleep should be replaced with a select that includes
// p.ctx.Done() so shutdown returns promptly (within milliseconds).
//
// Today: this test fails because close() takes longer than 500ms after
// ctx cancellation (it sleeps through the cancellation).
func TestTCPPort_AcceptRetrySleepNotCtxAware_BUG(t *testing.T) {
	t.Parallel()

	realLn, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer realLn.Close()

	// Inject enough transient errors to keep the retry/sleep path busy.
	flaky := &flakyAcceptListener{
		Listener:       realLn,
		failsRemaining: maxTCPAcceptRetries + 5,
	}

	ctx, cancel := context.WithCancel(context.Background())
	pconfig := model.PortConfig{ProxyProtocol: "tcp"}
	tp := newPortTCP(ctx, pconfig, zerolog.Nop(), nil, "", "")

	go func() { _ = tp.startWithListener(flaky) }()
	defer tp.close()

	// Wait until the retry path has consumed at least one Accept (and entered
	// the time.Sleep). Once failsRemaining decreases, the first sleep is active.
	require.Eventually(t, func() bool {
		flaky.mu.Lock()
		defer flaky.mu.Unlock()
		return flaky.failsRemaining < maxTCPAcceptRetries+5
	}, 2*time.Second, 5*time.Millisecond)

	// Cancel ctx to trigger shutdown. The bug: the active time.Sleep ignores
	// ctx and continues for its full duration.
	start := time.Now()
	cancel()

	closeDone := make(chan struct{})
	go func() {
		_ = tp.close()
		close(closeDone)
	}()

	select {
	case <-closeDone:
		elapsed := time.Since(start)
		// After fix: elapsed < ~50ms (select unblocks immediately on ctx.Done).
		// Today: elapsed ≈ current sleep duration (1s on first retry, up to 5s).
		// Use 500ms threshold so the test fails reliably on the bug while
		// remaining stable under CI scheduling jitter.
		require.Less(t, elapsed, 500*time.Millisecond,
			"BUG: tcpPort.close() took %v after ctx cancel — "+
				"time.Sleep in accept retry path is not ctx-aware (port.go:360), "+
				"delaying shutdown by up to 15s during transient Accept errors.", elapsed)
	case <-time.After(10 * time.Second):
		t.Fatal("BUG: tcpPort.close() did not return within 10s — " +
			"time.Sleep blocks shutdown indefinitely when Accept keeps failing")
	}
}
