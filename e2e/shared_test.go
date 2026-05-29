// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

//go:build e2e

package e2e

import (
	"bufio"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Shared Mode Helpers ---

// sharedHTTPLabels returns Docker labels for a container proxied via shared
// tsnet mode using HTTP (RouteHTTPHost routing).
func sharedHTTPLabels(name, domain string) map[string]string {
	return map[string]string{
		"tsdproxy.enable":    "true",
		"tsdproxy.name":      name,
		"tsdproxy.domain":    domain,
		"tsdproxy.ephemeral": "true",
		"tsdproxy.port.http": "80/http:80/http",
	}
}

// sharedHTTPSLabels returns Docker labels for a container proxied via shared
// tsnet mode using HTTPS (SNI routing) with a custom domain.
func sharedHTTPSLabels(name, domain string) map[string]string {
	return map[string]string{
		"tsdproxy.enable":     "true",
		"tsdproxy.name":       name,
		"tsdproxy.domain":     domain,
		"tsdproxy.ephemeral":  "true",
		"tsdproxy.port.https": "443/https:80/http",
	}
}

// sharedServerAddr returns the tsnet dial address for a shared server.
func sharedServerAddr(sharedHost, magicDNSSuffix string, port int) string {
	return fmt.Sprintf("%s.%s:%d", sharedHost, magicDNSSuffix, port)
}

// getSharedHTTP dials the shared server at sharedAddr (host:port) but sends
// an HTTP request with the Host header set to domain. Used for shared mode
// HTTP routing where the dial target (shared server) differs from the
// proxy's registered domain.
func getSharedHTTP(ctx context.Context, client *TSNetClient, sharedAddr, domain string) (*http.Response, error) {
	conn, err := client.server.Dial(ctx, "tcp", sharedAddr)
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", sharedAddr, err)
	}

	targetURL := fmt.Sprintf("http://%s/", domain)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
	if err != nil {
		conn.Close()
		return nil, err
	}
	req.Header.Set("Connection", "close")

	if err := req.Write(conn); err != nil {
		conn.Close()
		return nil, fmt.Errorf("write request: %w", err)
	}

	resp, err := http.ReadResponse(bufio.NewReader(conn), req)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("read response: %w", err)
	}
	resp.Body = &connCloser{body: resp.Body, conn: conn}
	return resp, nil
}

// getSharedHTTPS dials the shared server at sharedAddr (host:port) but
// sends a TLS ClientHello with SNI set to domain. Used for shared mode
// HTTPS routing with custom domains (Cloudflare DNS + ACME TLS).
func getSharedHTTPS(ctx context.Context, client *TSNetClient, sharedAddr, domain string) (*http.Response, error) {
	conn, err := client.server.Dial(ctx, "tcp", sharedAddr)
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", sharedAddr, err)
	}

	tlsConn := tls.Client(conn, &tls.Config{
		ServerName:         domain,
		InsecureSkipVerify: true, //nolint:gosec // E2E test: staging certs are untrusted
	})
	if err := tlsConn.Handshake(); err != nil {
		conn.Close()
		return nil, fmt.Errorf("TLS handshake: %w", err)
	}

	targetURL := fmt.Sprintf("https://%s/", domain)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
	if err != nil {
		conn.Close()
		return nil, err
	}
	req.Header.Set("Connection", "close")

	if err := req.Write(tlsConn); err != nil {
		conn.Close()
		return nil, fmt.Errorf("write request: %w", err)
	}

	resp, err := http.ReadResponse(bufio.NewReader(tlsConn), req)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("read response: %w", err)
	}
	resp.Body = &connCloser{body: resp.Body, conn: conn}
	return resp, nil
}

// waitForSharedHTTP polls until a shared HTTP proxy is reachable.
func waitForSharedHTTP(t *testing.T, ctx context.Context, client *TSNetClient, sharedAddr, domain string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			t.Fatalf("context cancelled waiting for shared HTTP proxy %s: %v", domain, ctx.Err())
		default:
		}
		resp, err := getSharedHTTP(ctx, client, sharedAddr, domain)
		if err == nil {
			resp.Body.Close()
			return
		}
		time.Sleep(2 * time.Second)
	}
	t.Fatalf("timeout waiting for shared HTTP proxy %s at %s", domain, sharedAddr)
}

// waitForSharedHTTPS polls until a shared HTTPS proxy is reachable.
func waitForSharedHTTPS(t *testing.T, ctx context.Context, client *TSNetClient, sharedAddr, domain string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			t.Fatalf("context cancelled waiting for shared HTTPS proxy %s: %v", domain, ctx.Err())
		default:
		}
		resp, err := getSharedHTTPS(ctx, client, sharedAddr, domain)
		if err == nil {
			resp.Body.Close()
			return
		}
		time.Sleep(2 * time.Second)
	}
	t.Fatalf("timeout waiting for shared HTTPS proxy %s at %s", domain, sharedAddr)
}

// verifySharedHTTP asserts that a shared HTTP proxy returns the expected content.
func verifySharedHTTP(t *testing.T, ctx context.Context, client *TSNetClient, sharedAddr, domain, expectedSubstring string) {
	t.Helper()
	resp, err := getSharedHTTP(ctx, client, sharedAddr, domain)
	require.NoError(t, err, "failed to GET shared proxy %s", domain)
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err, "failed to read response body")
	require.Equal(t, http.StatusOK, resp.StatusCode,
		"unexpected status code from %s: body=%s", domain, string(body))
	require.Contains(t, string(body), expectedSubstring,
		"response from %s does not contain expected substring", domain)
}

// verifySharedHTTPS asserts that a shared HTTPS proxy returns the expected content.
func verifySharedHTTPS(t *testing.T, ctx context.Context, client *TSNetClient, sharedAddr, domain, expectedSubstring string) {
	t.Helper()
	resp, err := getSharedHTTPS(ctx, client, sharedAddr, domain)
	require.NoError(t, err, "failed to GET shared HTTPS proxy %s", domain)
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err, "failed to read response body")
	require.Equal(t, http.StatusOK, resp.StatusCode,
		"unexpected status code from %s: body=%s", domain, string(body))
	require.Contains(t, string(body), expectedSubstring,
		"response from %s does not contain expected substring", domain)
}

// --- Tests ---

// TestSharedModeRestartCycle tests the shared tsnet server lifecycle:
// start proxies → verify → stop all → verify unreachable → start new proxies → verify.
// Uses HTTP routing (RouteHTTPHost) to avoid external DNS/TLS dependencies,
// so only TSDPROXY_E2E_AUTHKEY is required (same as TestBasicProxy).
func TestSharedModeRestartCycle(t *testing.T) {
	authKey := requireTailscaleAuth(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	// Create test client first to discover MagicDNSSuffix for dial target.
	client := NewTSNetClient(t, authKey)

	ts := time.Now().UnixNano()
	sharedHost := fmt.Sprintf("e2e-shared-rst-%d", ts)

	httpPort := getFreePort()
	tmpDir := e2eTestDataDir(t)
	dataDir := filepath.Join(tmpDir, "data")
	require.NoError(t, os.MkdirAll(dataDir, 0o755))

	configContent := fmt.Sprintf(`defaultProxyProvider: shared
docker:
  local:
    targetHostname: "172.17.0.1"
tailscale:
  providers:
    shared:
      shared: true
      hostname: %q
      authKey: %q
      tags: %q
  dataDir: %q
http:
  hostname: "0.0.0.0"
  port: %d
log:
  level: debug
  json: false
proxyAccessLog: true
`, sharedHost, authKey, tsTags, dataDir, httpPort)

	_ = startTSDProxyRawConfig(t, configContent, httpPort, tmpDir, dataDir)

	addr := sharedServerAddr(sharedHost, client.MagicDNSSuffix, 80)

	// --- Phase 1: Start 2 proxies and verify both reachable ---

	domain1 := fmt.Sprintf("app1-%d", ts)
	domain2 := fmt.Sprintf("app2-%d", ts)
	name1 := fmt.Sprintf("e2e-shared-rst-1-%d", ts)
	name2 := fmt.Sprintf("e2e-shared-rst-2-%d", ts)

	ctr1 := StartContainer(t, ContainerConfig{Labels: sharedHTTPLabels(name1, domain1)})
	ctr2 := StartContainer(t, ContainerConfig{Labels: sharedHTTPLabels(name2, domain2)})

	waitForSharedHTTP(t, ctx, client, addr, domain1, 120*time.Second)
	waitForSharedHTTP(t, ctx, client, addr, domain2, 120*time.Second)

	verifySharedHTTP(t, ctx, client, addr, domain1, "Welcome to nginx!")
	verifySharedHTTP(t, ctx, client, addr, domain2, "Welcome to nginx!")

	// --- Phase 2: Stop both containers and verify unreachable ---

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer stopCancel()
	require.NoError(t, ctr1.Stop(stopCtx, nil), "failed to stop container 1")
	require.NoError(t, ctr2.Stop(stopCtx, nil), "failed to stop container 2")

	assert.Eventually(t, func() bool {
		verifyCtx, verifyCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer verifyCancel()
		resp, err := getSharedHTTP(verifyCtx, client, addr, domain1)
		if err != nil {
			return true
		}
		resp.Body.Close()
		return false
	}, 45*time.Second, 2*time.Second, "expected proxy 1 (%s) to become unreachable after container stop", domain1)

	assert.Eventually(t, func() bool {
		verifyCtx, verifyCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer verifyCancel()
		resp, err := getSharedHTTP(verifyCtx, client, addr, domain2)
		if err != nil {
			return true
		}
		resp.Body.Close()
		return false
	}, 45*time.Second, 2*time.Second, "expected proxy 2 (%s) to become unreachable after container stop", domain2)

	// --- Phase 3: Start 2 NEW proxies on the same shared server ---

	domain3 := fmt.Sprintf("app3-%d", ts)
	domain4 := fmt.Sprintf("app4-%d", ts)
	name3 := fmt.Sprintf("e2e-shared-rst-3-%d", ts)
	name4 := fmt.Sprintf("e2e-shared-rst-4-%d", ts)

	StartContainer(t, ContainerConfig{Labels: sharedHTTPLabels(name3, domain3)})
	StartContainer(t, ContainerConfig{Labels: sharedHTTPLabels(name4, domain4)})

	waitForSharedHTTP(t, ctx, client, addr, domain3, 120*time.Second)
	waitForSharedHTTP(t, ctx, client, addr, domain4, 120*time.Second)

	verifySharedHTTP(t, ctx, client, addr, domain3, "Welcome to nginx!")
	verifySharedHTTP(t, ctx, client, addr, domain4, "Welcome to nginx!")

	// Verify old proxies are still unreachable.
	assert.Eventually(t, func() bool {
		verifyCtx, verifyCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer verifyCancel()
		resp, err := getSharedHTTP(verifyCtx, client, addr, domain1)
		if err != nil {
			return true
		}
		resp.Body.Close()
		return false
	}, 10*time.Second, 2*time.Second, "expected old proxy 1 (%s) to remain unreachable", domain1)
}

// TestSharedModeHTTPSWithCloudflare tests shared tsnet mode with Cloudflare DNS
// and ACME TLS provisioning (staging CA). Verifies SNI routing, DNS record
// creation, and TLS certificate provisioning end-to-end.
//
// Requires: CF_API_TOKEN, CF_DOMAIN, TSDPROXY_E2E_AUTHKEY.
func TestSharedModeHTTPSWithCloudflare(t *testing.T) {
	requireCloudflare(t)
	authKey := requireTailscaleAuth(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// Create test client first to discover MagicDNSSuffix.
	client := NewTSNetClient(t, authKey)

	ts := time.Now().UnixNano()
	sharedHost := fmt.Sprintf("e2e-shared-cf-%d", ts)

	httpPort := getFreePort()
	tmpDir := e2eTestDataDir(t)
	dataDir := filepath.Join(tmpDir, "data")
	require.NoError(t, os.MkdirAll(dataDir, 0o755))

	// Shared mode with Cloudflare DNS + ACME staging TLS.
	// cleanupDNS: true ensures CNAME records are removed when proxies stop.
	configContent := fmt.Sprintf(`defaultProxyProvider: shared
defaultDNSProvider: cloudflare
defaultTLSProvider: acme
docker:
  local:
    targetHostname: "172.17.0.1"
tailscale:
  providers:
    shared:
      shared: true
      hostname: %q
      authKey: %q
      tags: %q
  dataDir: %q
dnsProviders:
  cloudflare:
    provider: cloudflare
    apiToken: %q
tlsProviders:
  acme:
    provider: acme
    email: "e2e-test@tsdproxy.dev"
    ca: "https://acme-staging-v02.api.letsencrypt.org/directory"
http:
  hostname: "0.0.0.0"
  port: %d
log:
  level: debug
  json: false
proxyAccessLog: true
cleanupDNS: true
`, sharedHost, authKey, tsTags, dataDir, cfApiToken, httpPort)

	_ = startTSDProxyRawConfig(t, configContent, httpPort, tmpDir, dataDir)

	addr := sharedServerAddr(sharedHost, client.MagicDNSSuffix, 443)

	// Use unique subdomains under the user's CF_DOMAIN.
	domain1 := fmt.Sprintf("e2e-shared-cf-1-%d.%s", ts, cfDomain)
	domain2 := fmt.Sprintf("e2e-shared-cf-2-%d.%s", ts, cfDomain)
	name1 := fmt.Sprintf("e2e-cf-app1-%d", ts)
	name2 := fmt.Sprintf("e2e-cf-app2-%d", ts)

	ctr1 := StartContainer(t, ContainerConfig{Labels: sharedHTTPSLabels(name1, domain1)})
	StartContainer(t, ContainerConfig{Labels: sharedHTTPSLabels(name2, domain2)})

	// Wait for HTTPS proxies — DNS propagation + ACME cert provisioning can
	// take 30-90s per proxy.
	waitForSharedHTTPS(t, ctx, client, addr, domain1, 180*time.Second)
	waitForSharedHTTPS(t, ctx, client, addr, domain2, 180*time.Second)

	verifySharedHTTPS(t, ctx, client, addr, domain1, "Welcome to nginx!")
	verifySharedHTTPS(t, ctx, client, addr, domain2, "Welcome to nginx!")

	// Stop one container and verify the other still works (isolation).
	stopCtx, stopCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer stopCancel()
	require.NoError(t, ctr1.Stop(stopCtx, nil), "failed to stop container 1")

	assert.Eventually(t, func() bool {
		verifyCtx, verifyCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer verifyCancel()
		resp, err := getSharedHTTPS(verifyCtx, client, addr, domain1)
		if err != nil {
			return true
		}
		resp.Body.Close()
		return false
	}, 45*time.Second, 2*time.Second, "expected stopped proxy %s to become unreachable", domain1)

	// Second proxy should still work after the first is stopped.
	verifySharedHTTPS(t, ctx, client, addr, domain2, "Welcome to nginx!")
}
