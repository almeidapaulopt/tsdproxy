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
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestHTTPMethodForwarding(t *testing.T) {
	authKey := requireTailscaleAuth(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		fmt.Fprintf(w, "method=%s body=%s", r.Method, string(body))
	}))
	t.Cleanup(backend.Close)

	hostname := fmt.Sprintf("e2e-methods-%d", time.Now().UnixNano())
	listPath := filepath.Join(e2eTestDataDir(t), "methods-list.yaml")
	WriteListProviderFile(t, listPath, map[string]ListEntry{
		hostname: {
			ProxyProvider: "default",
			Tailscale:     ListTailscale{Ephemeral: true},
			Dashboard:     ListDashboard{Visible: true, Label: "Methods Test"},
			Ports: map[string]ListPort{
				"80/http": {Targets: []string{backend.URL}},
			},
		},
	})

	httpPort := getFreePort()
	tmpDir := e2eTestDataDir(t)
	dataDir := filepath.Join(tmpDir, "data")
	require.NoError(t, os.MkdirAll(dataDir, 0o755))

	configContent := fmt.Sprintf(`defaultProxyProvider: default
docker:
  local:
    host: "unix:///var/run/docker.sock"
    targetHostname: "172.17.0.1"
lists:
  methodslist:
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

	proxy := startTSDProxyRawConfig(t, configContent, httpPort, tmpDir, dataDir)
	_ = proxy

	client := NewTSNetClient(t, authKey)
	proxyURL := client.ProxyHTTPURL(hostname)
	WaitForProxyReachable(t, ctx, client, proxyURL, 60*time.Second)

	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete} {
		t.Run(method, func(t *testing.T) {
			body := fmt.Sprintf("test-body-%s", strings.ToLower(method))
			respBody := doHTTPMethod(t, ctx, client, proxyURL, method, body)
			expected := fmt.Sprintf("method=%s body=%s", method, body)
			require.Contains(t, respBody, expected,
				"expected %q, got %q", expected, respBody)
		})
	}
}

// doHTTPMethod sends an HTTP request with a body via the tsnet client.
func doHTTPMethod(t *testing.T, ctx context.Context, client *TSNetClient, targetURL, method, body string) string {
	t.Helper()

	parsed, err := url.Parse(targetURL)
	require.NoError(t, err)

	addr := dialHost(parsed)
	conn, err := client.Dial(ctx, "tcp", addr)
	require.NoError(t, err)
	defer conn.Close()

	var rw io.ReadWriter = conn

	if parsed.Scheme == "https" {
		tlsConn := tls.Client(conn, &tls.Config{
			ServerName:         parsed.Hostname(),
			InsecureSkipVerify: true,
		})
		require.NoError(t, tlsConn.Handshake(), "TLS handshake failed")
		rw = tlsConn
	}

	req, err := http.NewRequestWithContext(ctx, method, targetURL, strings.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "text/plain")
	req.Header.Set("Connection", "close")

	require.NoError(t, req.Write(rw), "failed to write request")

	resp, err := http.ReadResponse(bufio.NewReader(rw), req)
	require.NoError(t, err, "failed to read response")
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return string(respBody)
}
