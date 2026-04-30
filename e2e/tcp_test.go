// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

//go:build e2e

package e2e_test

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/almeidapaulopt/tsdproxy/internal/config"
	"github.com/almeidapaulopt/tsdproxy/internal/model"
	"github.com/almeidapaulopt/tsdproxy/internal/proxymanager"
	"github.com/almeidapaulopt/tsdproxy/internal/proxyproviders/tailscale"
	"github.com/rs/zerolog"
	"tailscale.com/tsnet"
)

func TestE2ETCPForward(t *testing.T) {
	authKey := os.Getenv("TSDPROXY_E2E_AUTHKEY")
	if authKey == "" {
		t.Skip("TSDPROXY_E2E_AUTHKEY not set, skipping e2e test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	tempDir := t.TempDir()
	log := testLogger(t)

	config.SetTestConfig(tempDir, authKey)

	echoLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to start echo server: %v", err)
	}
	defer echoLn.Close()
	go serveEcho(echoLn)

	hostname := fmt.Sprintf("e2e-tcp-%d", time.Now().UnixNano())

	targetURL, err := url.Parse("tcp://" + echoLn.Addr().String())
	if err != nil {
		t.Fatalf("failed to parse target URL: %v", err)
	}

	portConfig := model.PortConfig{
		ProxyProtocol: "tcp",
		ProxyPort:     443,
	}
	portConfig.AddTarget(targetURL)

	proxyCfg, err := model.NewConfig()
	if err != nil {
		t.Fatalf("failed to create proxy config: %v", err)
	}
	proxyCfg.Hostname = hostname
	proxyCfg.Ports = model.PortConfigList{"tcp": portConfig}
	proxyCfg.Tailscale.Ephemeral = true
	proxyCfg.ProxyProvider = "default"

	tsProvider, err := tailscale.New(log, "default", config.Config.Tailscale.Providers["default"])
	if err != nil {
		t.Fatalf("failed to create tailscale provider: %v", err)
	}

	proxy, err := proxymanager.NewProxy(log, proxyCfg, tsProvider)
	if err != nil {
		t.Fatalf("failed to create proxy: %v", err)
	}
	proxy.Start()

	if !waitForProxyStatus(ctx, t, proxy, model.ProxyStatusRunning) {
		proxy.Close()
		t.Fatalf("proxy did not reach Running status, current: %v", proxy.GetStatus())
	}

	proxyURL := proxy.GetURL()
	u, err := url.Parse(proxyURL)
	if err != nil {
		proxy.Close()
		t.Fatalf("failed to parse proxy URL %q: %v", proxyURL, err)
	}
	dnsName := u.Hostname()
	t.Logf("proxy running at %s", dnsName)

	clientServer := &tsnet.Server{
		Hostname:  fmt.Sprintf("e2e-client-%d", time.Now().UnixNano()),
		AuthKey:   authKey,
		Dir:       filepath.Join(tempDir, "client"),
		Ephemeral: true,
	}
	defer clientServer.Close()

	if _, err := clientServer.Up(ctx); err != nil {
		proxy.Close()
		t.Fatalf("client tsnet up: %v", err)
	}

	dialAddr := fmt.Sprintf("%s:443", dnsName)
	conn, err := clientServer.Dial(ctx, "tcp", dialAddr)
	if err != nil {
		proxy.Close()
		t.Fatalf("failed to dial proxy %s: %v", dialAddr, err)
	}
	defer conn.Close()

	message := []byte("hello from e2e test over tailscale tcp")
	if err := conn.SetDeadline(time.Now().Add(10 * time.Second)); err != nil {
		t.Fatalf("failed to set deadline: %v", err)
	}
	if _, err := conn.Write(message); err != nil {
		t.Fatalf("failed to write: %v", err)
	}

	buf := make([]byte, len(message))
	n, err := io.ReadFull(conn, buf)
	if err != nil {
		t.Fatalf("failed to read: %v", err)
	}
	if string(buf[:n]) != string(message) {
		t.Fatalf("expected %q, got %q", message, buf[:n])
	}

	t.Logf("echo verified: %q", buf[:n])

	conn.Close()
	proxy.Close()
}

func serveEcho(ln net.Listener) {
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
}

func waitForProxyStatus(ctx context.Context, t *testing.T, proxy *proxymanager.Proxy, target model.ProxyStatus) bool {
	t.Helper()

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return false
		case <-ticker.C:
			s := proxy.GetStatus()
			if s == target {
				return true
			}
			if s == model.ProxyStatusError || s == model.ProxyStatusStopped {
				t.Logf("proxy entered terminal state: %v", s)
				return false
			}
		}
	}
}

func testLogger(t *testing.T) zerolog.Logger {
	t.Helper()
	writer := zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: time.RFC3339}
	return zerolog.New(writer).With().Timestamp().Logger()
}
