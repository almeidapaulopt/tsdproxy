// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestContainerAuthKeyLabel(t *testing.T) {
	authKey := requireTailscaleAuth(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

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
    default: {}
  dataDir: %q
http:
  hostname: "0.0.0.0"
  port: %d
log:
  level: debug
  json: false
proxyAccessLog: true
`, dataDir, httpPort)

	proxy := startTSDProxyRawConfig(t, configContent, httpPort, tmpDir, dataDir)

	hostname := fmt.Sprintf("e2e-authkey-label-%d", time.Now().UnixNano())
	StartContainer(t, ContainerConfig{
		Labels: map[string]string{
			"tsdproxy.enable":    "true",
			"tsdproxy.ephemeral": "true",
			"tsdproxy.name":      hostname,
			"tsdproxy.authkey":   authKey,
			"tsdproxy.port.http": "80/http:80/http",
		},
	})

	client := NewTSNetClient(t, authKey)
	proxyURL := client.ProxyHTTPURL(hostname)

	WaitForProxyReachable(t, ctx, client, proxyURL, 90*time.Second)
	VerifyHTTPResponse(t, ctx, client, proxyURL, "Welcome to nginx!")
	_ = proxy
}

func TestContainerAuthKeyFileLabel(t *testing.T) {
	authKey := requireTailscaleAuth(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	httpPort := getFreePort()
	tmpDir := e2eTestDataDir(t)
	dataDir := filepath.Join(tmpDir, "data")
	require.NoError(t, os.MkdirAll(dataDir, 0o755))

	authKeyFile := filepath.Join(tmpDir, "container-authkey")
	require.NoError(t, os.WriteFile(authKeyFile, []byte(authKey), 0o600))

	// Configure provider with NO auth key — containers must supply their own.
	configContent := fmt.Sprintf(`defaultProxyProvider: default
docker:
  local:
    targetHostname: "172.17.0.1"
tailscale:
  providers:
    default: {}
  dataDir: %q
http:
  hostname: "0.0.0.0"
  port: %d
log:
  level: debug
  json: false
proxyAccessLog: true
`, dataDir, httpPort)

	proxy := startTSDProxyRawConfig(t, configContent, httpPort, tmpDir, dataDir)

	hostname := fmt.Sprintf("e2e-authkeyfile-label-%d", time.Now().UnixNano())

	StartContainer(t, ContainerConfig{
		Labels: map[string]string{
			"tsdproxy.enable":      "true",
			"tsdproxy.ephemeral":   "true",
			"tsdproxy.name":        hostname,
			"tsdproxy.authkeyfile": authKeyFile,
			"tsdproxy.port.http":   "80/http:80/http",
		},
	})

	client := NewTSNetClient(t, authKey)
	proxyURL := client.ProxyHTTPURL(hostname)

	WaitForProxyReachable(t, ctx, client, proxyURL, 90*time.Second)
	VerifyHTTPResponse(t, ctx, client, proxyURL, "Welcome to nginx!")
	_ = proxy
}
