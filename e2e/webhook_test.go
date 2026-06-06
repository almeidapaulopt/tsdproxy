// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

//go:build e2e

package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type webhookEvent struct {
	Proxy string `json:"proxy"`
	Status string `json:"status"`
}

func TestWebhookOnProxyStart(t *testing.T) {
	authKey := requireTailscaleAuth(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	var (
		mu       sync.Mutex
		received []webhookEvent
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var event webhookEvent
		if err := json.NewDecoder(r.Body).Decode(&event); err != nil {
			return
		}
		mu.Lock()
		received = append(received, event)
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	httpPort := getFreePort()
	tmpDir := e2eTestDataDir(t)
	dataDir := filepath.Join(tmpDir, "data")
	require.NoError(t, os.MkdirAll(dataDir, 0o755))

	configContent := fmt.Sprintf(`defaultProxyProvider: default
docker:
  local:
    targetHostname: "172.17.0.1"
tailscale:
  providers:
    default:
      authKey: %q
      tags: %q
  dataDir: %q
webhooks:
  - url: %q
    type: generic
    events: ["*"]
http:
  hostname: "0.0.0.0"
  port: %d
log:
  level: debug
  json: false
proxyAccessLog: true
`, authKey, tsTags, dataDir, srv.URL, httpPort)

	startTSDProxyRawConfig(t, configContent, httpPort, tmpDir, dataDir)

	hostname := fmt.Sprintf("e2e-webhook-start-%d", time.Now().UnixNano())
	StartContainer(t, ContainerConfig{Labels: httpLabels(hostname)})

	client := NewTSNetClient(t, authKey)
	proxyURL := client.ProxyHTTPURL(hostname)
	WaitForProxyReachable(t, ctx, client, proxyURL, 90*time.Second)

	assert.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		for _, e := range received {
			if e.Proxy == hostname {
				return true
			}
		}
		return false
	}, 30*time.Second, 2*time.Second, "expected webhook event for proxy %q", hostname)
}

func TestWebhookOnProxyStop(t *testing.T) {
	authKey := requireTailscaleAuth(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	var (
		mu       sync.Mutex
		received []webhookEvent
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var event webhookEvent
		if err := json.NewDecoder(r.Body).Decode(&event); err != nil {
			return
		}
		mu.Lock()
		received = append(received, event)
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	httpPort := getFreePort()
	tmpDir := e2eTestDataDir(t)
	dataDir := filepath.Join(tmpDir, "data")
	require.NoError(t, os.MkdirAll(dataDir, 0o755))

	configContent := fmt.Sprintf(`defaultProxyProvider: default
docker:
  local:
    targetHostname: "172.17.0.1"
tailscale:
  providers:
    default:
      authKey: %q
      tags: %q
  dataDir: %q
webhooks:
  - url: %q
    type: generic
    events: ["*"]
http:
  hostname: "0.0.0.0"
  port: %d
log:
  level: debug
  json: false
proxyAccessLog: true
`, authKey, tsTags, dataDir, srv.URL, httpPort)

	startTSDProxyRawConfig(t, configContent, httpPort, tmpDir, dataDir)

	hostname := fmt.Sprintf("e2e-webhook-stop-%d", time.Now().UnixNano())
	ctr := StartContainer(t, ContainerConfig{Labels: httpLabels(hostname)})

	client := NewTSNetClient(t, authKey)
	proxyURL := client.ProxyHTTPURL(hostname)
	WaitForProxyReachable(t, ctx, client, proxyURL, 90*time.Second)

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer stopCancel()
	require.NoError(t, ctr.Stop(stopCtx, nil))

	assert.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		for _, e := range received {
			if e.Proxy == hostname && (e.Status == "stopped" || e.Status == "Stopping") {
				return true
			}
		}
		return false
	}, 60*time.Second, 2*time.Second, "expected webhook stop event for proxy %q", hostname)
}
