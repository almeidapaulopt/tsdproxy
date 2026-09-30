// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

//go:build e2e

package e2e

import (
	"bufio"
	"context"
	"fmt"
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

func startSSEBackend(t *testing.T, eventCount int, interval time.Duration) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		require.True(t, ok, "ResponseWriter must support Flusher")
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		for i := range eventCount {
			fmt.Fprintf(w, "data: event-%d\n\n", i)
			flusher.Flush()
			time.Sleep(interval)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestSSEThroughProxy(t *testing.T) {
	authKey := requireTailscaleAuth(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	backend := startSSEBackend(t, 3, 100*time.Millisecond)

	hostname := fmt.Sprintf("e2e-sse-%d", time.Now().UnixNano())

	listFilePath := GenerateListProviderFile(t, map[string]ListEntry{
		hostname: {
			ProxyProvider: "default",
			Tailscale:     ListTailscale{Ephemeral: true},
			Dashboard:     ListDashboard{Visible: true, Label: "SSE E2E"},
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
    targetHostname: "172.17.0.1"
lists:
  sse:
    filename: %q
tailscale:
  providers:
    default:
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
`, listFilePath, authKey, tsTags, dataDir, httpPort)

	startTSDProxyRawConfig(t, configContent, httpPort, tmpDir, dataDir)

	client := NewTSNetClient(t, authKey)
	proxyURL := client.ProxyHTTPURL(hostname)
	WaitForProxyReachable(t, ctx, client, proxyURL, 90*time.Second)

	parsed, err := url.Parse(proxyURL)
	require.NoError(t, err)
	conn, err := client.Dial(ctx, "tcp", dialHost(parsed))
	require.NoError(t, err)
	defer conn.Close()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, proxyURL, nil)
	require.NoError(t, err)
	req.Header.Set("Accept", "text/event-stream")
	require.NoError(t, err)
	require.NoError(t, req.Write(conn))

	resp, err := http.ReadResponse(bufio.NewReader(conn), req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, "text/event-stream", resp.Header.Get("Content-Type"))

	scanner := bufio.NewScanner(resp.Body)
	var events []string
	deadline := time.Now().Add(30 * time.Second)
	for scanner.Scan() && time.Now().Before(deadline) {
		line := scanner.Text()
		if strings.HasPrefix(line, "data: ") {
			events = append(events, strings.TrimPrefix(line, "data: "))
		}
		if len(events) == 3 {
			break
		}
	}
	require.Equal(t, []string{"event-0", "event-1", "event-2"}, events,
		"expected 3 SSE events in order through proxy")
}
