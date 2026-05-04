// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultPortFormat(t *testing.T) {
	authKey := requireTailscaleAuth(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	_ = StartTSDProxy(t, TSDProxyConfig{
  AuthKey: authKey,
  Tags: tsTags,
})

	hostname := fmt.Sprintf("e2e-default-port-%d", time.Now().UnixNano())

	StartContainer(t, ContainerConfig{
		Image: testContainerImage,
		Labels: map[string]string{
			"tsdproxy.enable":    "true",
			"tsdproxy.ephemeral": "true",
			"tsdproxy.name":      hostname,
			"tsdproxy.port.web":  "443:80",
			"tsdproxy.port.http": "80/http:80/http",
		},
	})

	client := NewTSNetClient(t, authKey)

	proxyURL := client.ProxyHTTPURL(hostname)
	WaitForProxyReachable(t, ctx, client, proxyURL, 90*time.Second)
	VerifyHTTPResponse(t, ctx, client, proxyURL, "Welcome to nginx!")
}

func TestExplicitProtocolPort(t *testing.T) {
	authKey := requireTailscaleAuth(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	_ = StartTSDProxy(t, TSDProxyConfig{
  AuthKey: authKey,
  Tags: tsTags,
})

	hostname := fmt.Sprintf("e2e-explicit-proto-%d", time.Now().UnixNano())

	StartContainer(t, ContainerConfig{
		Image: testContainerImage,
		Labels: map[string]string{
			"tsdproxy.enable":    "true",
			"tsdproxy.ephemeral": "true",
			"tsdproxy.name":      hostname,
			"tsdproxy.port.web":  "443/https:80/http",
			"tsdproxy.port.http": "80/http:80/http",
		},
	})

	client := NewTSNetClient(t, authKey)

	proxyURL := client.ProxyHTTPURL(hostname)
	WaitForProxyReachable(t, ctx, client, proxyURL, 90*time.Second)
	VerifyHTTPResponse(t, ctx, client, proxyURL, "Welcome to nginx!")
}

func TestCustomTargetPort(t *testing.T) {
	authKey := requireTailscaleAuth(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	_ = StartTSDProxy(t, TSDProxyConfig{
  AuthKey: authKey,
  Tags: tsTags,
})

	hostname := fmt.Sprintf("e2e-custom-port-%d", time.Now().UnixNano())

	StartContainer(t, ContainerConfig{
		Image:    "nginxinc/nginx-unprivileged:latest",
		WaitPort: "8080/tcp",
		Labels: map[string]string{
			"tsdproxy.enable":    "true",
			"tsdproxy.ephemeral": "true",
			"tsdproxy.name":      hostname,
			"tsdproxy.port.web":  "443:8080/http",
			"tsdproxy.port.http": "80/http:8080/http",
		},
	})

	client := NewTSNetClient(t, authKey)

	proxyURL := client.ProxyHTTPURL(hostname)
	WaitForProxyReachable(t, ctx, client, proxyURL, 90*time.Second)
	VerifyHTTPResponse(t, ctx, client, proxyURL, "Welcome to nginx!")
}

func TestMultiPortContainer(t *testing.T) {
	authKey := requireTailscaleAuth(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	_ = StartTSDProxy(t, TSDProxyConfig{
  AuthKey: authKey,
  Tags: tsTags,
})

	hostname := fmt.Sprintf("e2e-multi-port-%d", time.Now().UnixNano())

	StartContainer(t, ContainerConfig{
		Image: testContainerImage,
		Labels: map[string]string{
			"tsdproxy.enable":    "true",
			"tsdproxy.ephemeral": "true",
			"tsdproxy.name":      hostname,
			"tsdproxy.port.web":  "443:80/http",
			"tsdproxy.port.http": "80/http:80/http",
		},
	})

	client := NewTSNetClient(t, authKey)

	proxyURL := client.ProxyHTTPURL(hostname)
	WaitForProxyReachable(t, ctx, client, proxyURL, 90*time.Second)

	resp, err := client.Get(ctx, proxyURL)
	require.NoError(t, err, "failed to GET %s", proxyURL)
	defer resp.Body.Close()

	assert.Equal(t, 200, resp.StatusCode, "expected 200 from %s", proxyURL)
}

func TestRedirectPort(t *testing.T) {
	authKey := requireTailscaleAuth(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	_ = StartTSDProxy(t, TSDProxyConfig{
  AuthKey: authKey,
  Tags: tsTags,
})

	hostname := fmt.Sprintf("e2e-redirect-%d", time.Now().UnixNano())

	StartContainer(t, ContainerConfig{
		Image: testContainerImage,
		Labels: map[string]string{
			"tsdproxy.enable":        "true",
			"tsdproxy.ephemeral":     "true",
			"tsdproxy.name":          hostname,
			"tsdproxy.port.redirect": "80/http->https://example.com",
		},
	})

	client := NewTSNetClient(t, authKey)

	// Build the redirect URL — port 80 is the redirect port listener.
	// GetNoFollowRedirect always uses TLS over tsnet regardless of URL scheme.
	redirectURL := fmt.Sprintf("http://%s.%s:80", hostname, client.MagicDNSSuffix)

	// Poll until the redirect port is reachable (default WaitForProxyReachable
	// checks HTTPS 443, so we poll port 80 directly via GetNoFollowRedirect).
	var reachable bool
	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			t.Fatalf("context cancelled waiting for redirect port: %v", ctx.Err())
		default:
		}

		resp, err := client.GetNoFollowRedirectHTTP(ctx, redirectURL)
		if err == nil {
			resp.Body.Close()
			reachable = true
			break
		}
		time.Sleep(2 * time.Second)
	}
	require.True(t, reachable, "redirect port %s was not reachable within timeout", redirectURL)

	resp, err := client.GetNoFollowRedirectHTTP(ctx, redirectURL)
	require.NoError(t, err, "failed to GET redirect URL %s", redirectURL)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusMovedPermanently, resp.StatusCode,
		"expected 301 redirect from %s, got %d", redirectURL, resp.StatusCode)
	assert.Contains(t, resp.Header.Get("Location"), "https://example.com",
		"expected Location header to contain target URL, got %q", resp.Header.Get("Location"))
}

func TestNoTLSValidateAcceptsSelfSignedCert(t *testing.T) {
	authKey := requireTailscaleAuth(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	srv := StartSelfSignedHTTPSServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "HTTPS OK")
	}))

	hostname := fmt.Sprintf("e2e-notlsvalidate-%d", time.Now().UnixNano())

	// Use HTTP proxy port (80/http) to avoid ACME cert requirement.
	// TLS validation is about the outgoing connection to the upstream target,
	// not the incoming proxy listener.
	listContent := fmt.Sprintf(`%s:
  proxyProvider: "default"
  tailscale:
    tags: %q
    ephemeral: true
  dashboard:
    visible: true
  ports:
    "80/http":
      targets:
        - %q
      tlsValidate: false
`, hostname, tsTags, srv.URL)

	listPath := filepath.Join(t.TempDir(), "list.yaml")
	require.NoError(t, os.WriteFile(listPath, []byte(listContent), 0o644))

	httpPort := getFreePort()
	tmpDir := e2eTestDataDir(t)
	dataDir := filepath.Join(tmpDir, "data")
	require.NoError(t, os.MkdirAll(dataDir, 0o755))

	configContent := fmt.Sprintf(`defaultProxyProvider: default
docker:
  local:
    host: "unix:///var/run/docker.sock"
    targetHostname: "172.31.0.1"
lists:
  testlist:
    filename: %q
tailscale:
  providers:
    default:
      authKey: %q
  dataDir: %q
http:
  hostname: "0.0.0.0"
  port: %d
log:
  level: debug
  json: false
proxyAccessLog: true
`, listPath, authKey, dataDir, httpPort)

	_ = startTSDProxyRawConfig(t, configContent, httpPort, tmpDir, dataDir)

	client := NewTSNetClient(t, authKey)

	proxyURL := client.ProxyHTTPURL(hostname)
	WaitForProxyReachable(t, ctx, client, proxyURL, 90*time.Second)
	VerifyHTTPResponse(t, ctx, client, proxyURL, "HTTPS OK")
}

func TestTLSValidateRejectsSelfSignedCert(t *testing.T) {
	authKey := requireTailscaleAuth(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	srv := StartSelfSignedHTTPSServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "HTTPS OK")
	}))

	hostname := fmt.Sprintf("e2e-tlsvalidate-%d", time.Now().UnixNano())

	// Use HTTP proxy port to avoid ACME cert requirement.
	// Explicitly set tlsValidate: true to test cert rejection.
	listContent := fmt.Sprintf(`%s:
  proxyProvider: "default"
  tailscale:
    tags: %q
    ephemeral: true
  dashboard:
    visible: true
  ports:
    "80/http":
      targets:
        - %q
      tlsValidate: true
`, hostname, tsTags, srv.URL)

	listPath := filepath.Join(t.TempDir(), "list.yaml")
	require.NoError(t, os.WriteFile(listPath, []byte(listContent), 0o644))

	httpPort := getFreePort()
	tmpDir := e2eTestDataDir(t)
	dataDir := filepath.Join(tmpDir, "data")
	require.NoError(t, os.MkdirAll(dataDir, 0o755))

	configContent := fmt.Sprintf(`defaultProxyProvider: default
docker:
  local:
    host: "unix:///var/run/docker.sock"
    targetHostname: "172.31.0.1"
lists:
  testlist:
    filename: %q
tailscale:
  providers:
    default:
      authKey: %q
  dataDir: %q
http:
  hostname: "0.0.0.0"
  port: %d
log:
  level: debug
  json: false
proxyAccessLog: true
`, listPath, authKey, dataDir, httpPort)

	_ = startTSDProxyRawConfig(t, configContent, httpPort, tmpDir, dataDir)

	client := NewTSNetClient(t, authKey)

	proxyURL := client.ProxyHTTPURL(hostname)
	WaitForProxyReachable(t, ctx, client, proxyURL, 90*time.Second)

	resp, err := client.Get(ctx, proxyURL)
	require.NoError(t, err, "failed to GET %s", proxyURL)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadGateway, resp.StatusCode,
		"expected 502 Bad Gateway due to self-signed cert rejection, got %d", resp.StatusCode)
}

func TestModernPortOptionNoTLSValidate(t *testing.T) {
	authKey := requireTailscaleAuth(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	_ = StartTSDProxy(t, TSDProxyConfig{
  AuthKey: authKey,
  Tags: tsTags,
})

	hostname := fmt.Sprintf("e2e-modern-notlsvalidate-%d", time.Now().UnixNano())
	StartSelfSignedHTTPSContainer(t, map[string]string{
		"tsdproxy.enable":    "true",
		"tsdproxy.ephemeral": "true",
		"tsdproxy.name":      hostname,
		"tsdproxy.port.http": "80/http:8443/https, no_tlsvalidate",
	})

	client := NewTSNetClient(t, authKey)
	proxyURL := client.ProxyHTTPURL(hostname)

	WaitForProxyReachable(t, ctx, client, proxyURL, 90*time.Second)
	VerifyHTTPResponse(t, ctx, client, proxyURL, "HTTPS OK")
}

func TestModernPortOptionTLSValidateRejectsSelfSigned(t *testing.T) {
	authKey := requireTailscaleAuth(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	srv := StartSelfSignedHTTPSServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "HTTPS OK")
	}))

	hostname := fmt.Sprintf("e2e-modern-tlsvalidate-%d", time.Now().UnixNano())

	listContent := fmt.Sprintf(`%s:
  proxyProvider: "default"
  tailscale:
    tags: %q
    ephemeral: true
  dashboard:
    visible: true
  ports:
    "80/http":
      targets:
        - %q
      tlsValidate: true
`, hostname, tsTags, srv.URL)

	listPath := filepath.Join(t.TempDir(), "list.yaml")
	require.NoError(t, os.WriteFile(listPath, []byte(listContent), 0o644))

	httpPort := getFreePort()
	tmpDir := e2eTestDataDir(t)
	dataDir := filepath.Join(tmpDir, "data")
	require.NoError(t, os.MkdirAll(dataDir, 0o755))

	configContent := fmt.Sprintf(`defaultProxyProvider: default
docker:
  local:
    host: "unix:///var/run/docker.sock"
    targetHostname: "172.31.0.1"
lists:
  testlist:
    filename: %q
tailscale:
  providers:
    default:
      authKey: %q
  dataDir: %q
http:
  hostname: "0.0.0.0"
  port: %d
log:
  level: debug
  json: false
proxyAccessLog: true
`, listPath, authKey, dataDir, httpPort)

	_ = startTSDProxyRawConfig(t, configContent, httpPort, tmpDir, dataDir)

	client := NewTSNetClient(t, authKey)
	proxyURL := client.ProxyHTTPURL(hostname)
	WaitForProxyReachable(t, ctx, client, proxyURL, 90*time.Second)

	resp, err := client.Get(ctx, proxyURL)
	require.NoError(t, err, "failed to GET %s", proxyURL)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadGateway, resp.StatusCode,
		"expected 502 Bad Gateway due to self-signed cert rejection, got %d", resp.StatusCode)
}
