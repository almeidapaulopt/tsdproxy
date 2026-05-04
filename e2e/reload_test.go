// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestListProviderReload(t *testing.T) {
	authKey := requireTailscaleAuth(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	backendOne := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "backend-one")
	}))
	t.Cleanup(backendOne.Close)

	backendTwo := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "backend-two")
	}))
	t.Cleanup(backendTwo.Close)

	hostname := fmt.Sprintf("e2e-reload-%d", time.Now().UnixNano())
	listPath := filepath.Join(e2eTestDataDir(t), "reload-list.yaml")
	WriteListProviderFile(t, listPath, map[string]ListEntry{
		hostname: {
			ProxyProvider: "default",
			Dashboard:     ListDashboard{Visible: true, Label: "Reload Test"},
			Ports: map[string]ListPort{
				"80/http": {Targets: []string{backendOne.URL}},
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

	WaitForProxyReachable(t, ctx, client, proxyURL, 60*time.Second)
	VerifyHTTPResponse(t, ctx, client, proxyURL, "backend-one")

	WriteListProviderFile(t, listPath, map[string]ListEntry{
		hostname: {
			ProxyProvider: "default",
			Dashboard:     ListDashboard{Visible: true, Label: "Reload Test"},
			Ports: map[string]ListPort{
				"80/http": {Targets: []string{backendTwo.URL}},
			},
		},
	})

	require.Eventually(t, func() bool {
		resp, err := client.Get(ctx, proxyURL)
		if err != nil {
			return false
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return false
		}
		return resp.StatusCode == http.StatusOK && string(body) == "backend-two"
	}, 45*time.Second, 2*time.Second, "expected list provider reload to switch backend target")

	WriteListProviderFile(t, listPath, map[string]ListEntry{})

	assert.Eventually(t, func() bool {
		verifyCtx, verifyCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer verifyCancel()
		resp, err := client.Get(verifyCtx, proxyURL)
		if err != nil {
			return true
		}
		resp.Body.Close()
		return false
	}, 45*time.Second, 2*time.Second, "expected proxy to be removed after list entry deletion")
}
