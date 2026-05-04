// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"io"
	"net/http/httptest"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"tailscale.com/ipn/ipnstate"
)

const (
	labelsStartupTimeout = 90 * time.Second
	labelsVerifyTimeout  = 30 * time.Second
)

func TestLegacyContainerPortLabel(t *testing.T) {
	authKey := requireTailscaleAuth(t)
	ctx, cancel := context.WithTimeout(context.Background(), labelsStartupTimeout)
	defer cancel()

	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "legacy port OK")
	}))
	t.Cleanup(backend.Close)

	backendPort, err := strconv.Atoi(mustURLPort(t, backend.URL))
	require.NoError(t, err, "failed to parse backend port")

	_ = StartTSDProxy(t, TSDProxyConfig{
  AuthKey:        authKey,
  TargetHostname: "127.0.0.1",
})

	hostname := fmt.Sprintf("legacy-port-%d", time.Now().UnixNano())

	StartContainer(t, ContainerConfig{
		Image:    "alpine",
		Cmd:      []string{"sleep", "3600"},
		WaitPort: "skip",
		Labels: map[string]string{
			"tsdproxy.enable":         "true",
			"tsdproxy.ephemeral":      "true",
			"tsdproxy.name":           hostname,
			"tsdproxy.container_port": strconv.Itoa(backendPort),
			"tsdproxy.scheme":         "http",
		},
	})

	client := NewTSNetClient(t, authKey)
	proxyURL := client.ProxyURL(hostname)

	WaitForProxyReachable(t, ctx, client, proxyURL, labelsVerifyTimeout)
	VerifyHTTPResponse(t, ctx, client, proxyURL, "legacy port OK")
}

// TestLegacyFunnelLabel verifies that the tsdproxy.funnel=true label enables
// Tailscale Funnel on the node. It checks both proxy reachability and that the
// node's capabilities include "funnel" via the peer API.
func TestLegacyFunnelLabel(t *testing.T) {
	authKey := requireTailscaleAuth(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "legacy funnel OK")
	}))
	t.Cleanup(backend.Close)

	backendPort, err := strconv.Atoi(mustURLPort(t, backend.URL))
	require.NoError(t, err, "failed to parse backend port")

	_ = StartTSDProxy(t, TSDProxyConfig{
  AuthKey:        authKey,
  TargetHostname: "127.0.0.1",
})

	hostname := fmt.Sprintf("legacy-funnel-%d", time.Now().UnixNano())

	StartContainer(t, ContainerConfig{
		Image:    "alpine",
		Cmd:      []string{"sleep", "3600"},
		WaitPort: "skip",
		Labels: map[string]string{
			"tsdproxy.enable":         "true",
			"tsdproxy.ephemeral":      "true",
			"tsdproxy.name":           hostname,
			"tsdproxy.container_port": strconv.Itoa(backendPort),
			"tsdproxy.funnel":         "true",
			"tsdproxy.scheme":         "http",
		},
	})

	client := NewTSNetClient(t, authKey)
	peer := waitForPeerByDNSName(t, ctx, client, hostname, 90*time.Second)
	t.Logf("peer %s capabilities: %v", hostname, peer.Capabilities)
	hasFunnel := false
	for _, cap := range peer.Capabilities {
		if string(cap) == "funnel" {
			hasFunnel = true
			break
		}
	}
	if !hasFunnel {
		t.Skipf("funnel capability not present on peer %s (capabilities: %v). "+
			"This test requires funnel to be enabled in the tailnet ACL policy.", hostname, peer.Capabilities)
	}

	proxyURL := client.ProxyURL(hostname)
	WaitForProxyReachable(t, ctx, client, proxyURL, 90*time.Second)
	VerifyHTTPResponse(t, ctx, client, proxyURL, "legacy funnel OK")
}

// TestLegacyTLSValidateLabel verifies that tlsValidate=false allows the proxy
// to connect to an upstream target using a self-signed TLS certificate.
// Without tlsValidate=false, the proxy would reject the self-signed cert and
// return 502 Bad Gateway. This test uses a list provider pointing to a
// self-signed HTTPS server to actually exercise the TLS validation code path.
func TestLegacyTLSValidateLabel(t *testing.T) {
	authKey := requireTailscaleAuth(t)
	ctx, cancel := context.WithTimeout(context.Background(), labelsStartupTimeout)
	defer cancel()

	srv := StartSelfSignedHTTPSServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "TLS validate OK")
	}))
	backendPort, err := strconv.Atoi(mustURLPort(t, srv.URL))
	require.NoError(t, err, "failed to parse backend port")

	hostname := fmt.Sprintf("legacy-tlsvalidate-%d", time.Now().UnixNano())
	_ = StartTSDProxy(t, TSDProxyConfig{
  AuthKey:        authKey,
  TargetHostname: "127.0.0.1",
})

	StartContainer(t, ContainerConfig{
		Image:    "alpine",
		Cmd:      []string{"sleep", "3600"},
		WaitPort: "skip",
		Labels: map[string]string{
			"tsdproxy.enable":         "true",
			"tsdproxy.ephemeral":      "true",
			"tsdproxy.name":           hostname,
			"tsdproxy.container_port": strconv.Itoa(backendPort),
			"tsdproxy.scheme":         "https",
			"tsdproxy.tlsvalidate":    "false",
		},
	})

	client := NewTSNetClient(t, authKey)
	proxyURL := client.ProxyURL(hostname)

	WaitForProxyReachable(t, ctx, client, proxyURL, labelsVerifyTimeout)
	VerifyHTTPResponse(t, ctx, client, proxyURL, "TLS validate OK")
}

// TestCustomTags verifies that the tsdproxy.tags label configures ACL tags
// on the Tailscale node. It checks both proxy reachability and that the node
// has the expected tag via the peer API.
func TestCustomTags(t *testing.T) {
	requireOAuth(t)
	authKey := requireTailscaleAuth(t)
	ctx, cancel := context.WithTimeout(context.Background(), labelsStartupTimeout)
	defer cancel()

	httpPort := getFreePort()
	tmpDir := e2eTestDataDir(t)
	dataDir := filepath.Join(tmpDir, "data")
	require.NoError(t, os.MkdirAll(dataDir, 0o755))

	configContent := generateConfig(configParams{
		HTTPPort:     httpPort,
		DataDir:      dataDir,
		ClientID:     tsClientID,
		ClientSecret: tsClientSecret,
	})

	_ = startTSDProxyRawConfig(t, configContent, httpPort, tmpDir, dataDir)

	hostname := fmt.Sprintf("custom-tags-%d", time.Now().UnixNano())

	StartContainer(t, ContainerConfig{
		Labels: map[string]string{
			"tsdproxy.enable":    "true",
			"tsdproxy.ephemeral": "true",
			"tsdproxy.name":      hostname,
			"tsdproxy.tags":      "tag:e2e-test",
			"tsdproxy.port.http": "80/http:80/http",
		},
	})

	client := NewTSNetClient(t, authKey)
	proxyURL := client.ProxyHTTPURL(hostname)

	WaitForProxyReachable(t, ctx, client, proxyURL, labelsVerifyTimeout)
	VerifyHTTPResponse(t, ctx, client, proxyURL, "Welcome to nginx!")

	peer := requirePeerByDNSName(t, ctx, client, hostname)
	if peer.Tags == nil {
		t.Fatalf("peer %s has nil Tags — the auth key must have tag:e2e-test in the ACL policy. "+
			"Peer capabilities: %v", hostname, peer.Capabilities)
	}
	assert.Contains(t, peer.Tags.AsSlice(), "tag:e2e-test", "expected tag:e2e-test on node %s", hostname)
}

// TestDashboardVisibleFalse verifies that a container with
// tsdproxy.dash.visible=false does NOT appear in the dashboard SSE stream,
// while a container with default visibility (visible=true) DOES appear.
func TestDashboardVisibleFalse(t *testing.T) {
	authKey := requireTailscaleAuth(t)
	ctx, cancel := context.WithTimeout(context.Background(), labelsStartupTimeout)
	defer cancel()

	proxy := StartTSDProxy(t, TSDProxyConfig{
  AuthKey: authKey,
})

	// Start hidden container (visible=false)
	suffix := time.Now().UnixNano()
	hostnameHidden := fmt.Sprintf("dash-hidden-%d", suffix)
	hostnameVisible := fmt.Sprintf("dash-visible-%d", suffix)

	StartContainer(t, ContainerConfig{
		Labels: map[string]string{
			"tsdproxy.enable":         "true",
			"tsdproxy.ephemeral":      "true",
			"tsdproxy.name":           hostnameHidden,
			"tsdproxy.container_port": "80",
			"tsdproxy.dash.visible":   "false",
			"tsdproxy.port.http":      "80/http:80/http",
		},
	})

	StartContainer(t, ContainerConfig{
		Labels: map[string]string{
			"tsdproxy.enable":         "true",
			"tsdproxy.ephemeral":      "true",
			"tsdproxy.name":           hostnameVisible,
			"tsdproxy.container_port": "80",
			"tsdproxy.port.http":      "80/http:80/http",
		},
	})

	client := NewTSNetClient(t, authKey)

	urlHidden := client.ProxyHTTPURL(hostnameHidden)
	urlVisible := client.ProxyHTTPURL(hostnameVisible)

	WaitForProxyReachable(t, ctx, client, urlHidden, labelsVerifyTimeout)
	WaitForProxyReachable(t, ctx, client, urlVisible, labelsVerifyTimeout)

	// Connect to the dashboard SSE stream and read the response
	sseBody := readSSEStream(t, proxy, "e2e-dash-visible-test")

	assert.NotContains(t, sseBody, hostnameHidden,
		"proxy with dash.visible=false should not appear in dashboard SSE stream")

	assert.Contains(t, sseBody, hostnameVisible,
		"proxy with default visibility should appear in dashboard SSE stream")
}

// TestDashboardCustomLabel verifies that custom dashboard labels
// (tsdproxy.dash.label and tsdproxy.dash.icon) are applied to the proxy
// and appear in the dashboard SSE stream.
func TestDashboardCustomLabel(t *testing.T) {
	authKey := requireTailscaleAuth(t)
	ctx, cancel := context.WithTimeout(context.Background(), labelsStartupTimeout)
	defer cancel()

	proxy := StartTSDProxy(t, TSDProxyConfig{
  AuthKey: authKey,
})

	hostname := fmt.Sprintf("dash-label-%d", time.Now().UnixNano())

	StartContainer(t, ContainerConfig{
		Labels: map[string]string{
			"tsdproxy.enable":         "true",
			"tsdproxy.ephemeral":      "true",
			"tsdproxy.name":           hostname,
			"tsdproxy.container_port": "80",
			"tsdproxy.dash.label":     "My Test Service",
			"tsdproxy.dash.icon":      "nginx",
			"tsdproxy.port.http":      "80/http:80/http",
		},
	})

	client := NewTSNetClient(t, authKey)
	proxyURL := client.ProxyHTTPURL(hostname)

	WaitForProxyReachable(t, ctx, client, proxyURL, labelsVerifyTimeout)

	sseBody := readSSEStream(t, proxy, "e2e-dash-label-test")

	assert.Contains(t, sseBody, "My Test Service",
		"custom dashboard label should appear in dashboard SSE stream")
	assert.Contains(t, sseBody, "/icons/nginx.svg",
		"custom dashboard icon should appear in dashboard SSE stream")
	assert.Contains(t, sseBody, hostname,
		"hostname should appear as element ID in dashboard SSE stream")

	logContent := proxy.ReadLogFile(t)
	assert.Contains(t, logContent, hostname,
		"tsdproxy log should contain evidence of the proxy being configured")
}

// TestContainerAccessLog verifies that when tsdproxy.containeraccesslog=true
// is set, HTTP access log entries are written to the tsdproxy log for
// requests proxied to that container.
func TestContainerAccessLog(t *testing.T) {
	authKey := requireTailscaleAuth(t)
	ctx, cancel := context.WithTimeout(context.Background(), labelsStartupTimeout)
	defer cancel()

	proxy := StartTSDProxy(t, TSDProxyConfig{
  AuthKey: authKey,
})

	hostname := fmt.Sprintf("access-log-%d", time.Now().UnixNano())

	StartContainer(t, ContainerConfig{
		Labels: map[string]string{
			"tsdproxy.enable":             "true",
			"tsdproxy.ephemeral":          "true",
			"tsdproxy.name":               hostname,
			"tsdproxy.container_port":     "80",
			"tsdproxy.containeraccesslog": "true",
			"tsdproxy.port.http":          "80/http:80/http",
		},
	})

	client := NewTSNetClient(t, authKey)
	proxyURL := client.ProxyHTTPURL(hostname)

	WaitForProxyReachable(t, ctx, client, proxyURL, labelsVerifyTimeout)

	VerifyHTTPResponse(t, ctx, client, proxyURL, "Welcome to nginx!")

	logContent := proxy.ReadLogFile(t)
	cleanLog := stripANSI(logContent)

	hasAccessLog := strings.Contains(cleanLog, "method=GET") &&
		strings.Contains(cleanLog, "status=") &&
		strings.Contains(cleanLog, "url=/")

	assert.True(t, hasAccessLog,
		"tsdproxy log should contain access log entries with method=GET, status=, and url=/ fields. Log content:\n%s",
		cleanLog)
}

func TestPortLabelsTakePrecedenceOverLegacy(t *testing.T) {
	authKey := requireTailscaleAuth(t)
	ctx, cancel := context.WithTimeout(context.Background(), labelsStartupTimeout)
	defer cancel()

	modernBackend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "modern-port-response")
	}))
	t.Cleanup(modernBackend.Close)

	legacyBackend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "legacy-port-response")
	}))
	t.Cleanup(legacyBackend.Close)

	modernPort, err := strconv.Atoi(mustURLPort(t, modernBackend.URL))
	require.NoError(t, err)

	legacyPort, err := strconv.Atoi(mustURLPort(t, legacyBackend.URL))
	require.NoError(t, err)

	_ = StartTSDProxy(t, TSDProxyConfig{
  AuthKey:        authKey,
  TargetHostname: "127.0.0.1",
})

	hostname := fmt.Sprintf("port-precedence-%d", time.Now().UnixNano())

	StartContainer(t, ContainerConfig{
		Image:    "alpine",
		Cmd:      []string{"sleep", "3600"},
		WaitPort: "skip",
		Labels: map[string]string{
			"tsdproxy.enable":         "true",
			"tsdproxy.ephemeral":      "true",
			"tsdproxy.name":           hostname,
			"tsdproxy.port.http":      fmt.Sprintf("80/http:%d/http", modernPort),
			"tsdproxy.container_port": strconv.Itoa(legacyPort),
			"tsdproxy.scheme":         "http",
		},
	})

	client := NewTSNetClient(t, authKey)
	proxyURL := client.ProxyHTTPURL(hostname)

	WaitForProxyReachable(t, ctx, client, proxyURL, labelsVerifyTimeout)
	VerifyHTTPResponse(t, ctx, client, proxyURL, "modern-port-response")
}

// requirePeerByDNSName is a test helper that calls GetPeerByDNSName and fails
// the test immediately if the peer cannot be found.
func requirePeerByDNSName(t *testing.T, ctx context.Context, client *TSNetClient, hostname string) *ipnstate.PeerStatus {
	t.Helper()
	peer, err := client.GetPeerByDNSName(ctx, hostname)
	require.NoError(t, err, "failed to find peer %s in tailnet", hostname)
	return peer
}

// readSSEStream connects to the dashboard SSE stream endpoint, reads the
// available data until the context expires, and returns the accumulated
// response body as a string.
func readSSEStream(t *testing.T, proxy *TSDProxyInstance, sessionID string) string {
	t.Helper()

	streamCtx, streamCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer streamCancel()

	req, err := http.NewRequestWithContext(streamCtx, http.MethodGet, proxy.BaseURL+"/stream", nil)
	require.NoError(t, err, "failed to create SSE stream request")
	req.Header.Set("X-Session-ID", sessionID)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err, "failed to connect to SSE stream")
	defer resp.Body.Close()

	// Read the SSE response body. The stream stays open until the context
	// expires, so io.ReadAll returns whatever was accumulated.
	body, _ := io.ReadAll(resp.Body)

	return string(body)
}

// stripANSI removes ANSI escape sequences from a string.
var ansiRe = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func stripANSI(s string) string {
	return ansiRe.ReplaceAllString(s, "")
}
