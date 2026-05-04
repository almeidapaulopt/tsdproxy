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

func TestPreventDuplicatesRecoversHostnameAfterStateLoss(t *testing.T) {
	requireOAuth(t)
	authKey := requireTailscaleAuth(t)
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	hostname := fmt.Sprintf("e2e-prevent-duplicates-%d", time.Now().UnixNano())
	tmpDir := e2eTestDataDir(t)
	dataDir := filepath.Join(tmpDir, "data")
	require.NoError(t, os.MkdirAll(dataDir, 0o755))

	configForPort := func(httpPort int) string {
		return fmt.Sprintf(`defaultProxyProvider: default
docker:
  local:
    targetHostname: "172.17.0.1"
tailscale:
  providers:
    default:
      clientId: %q
      clientSecret: %q
      tags: %q
      preventDuplicates: true
  dataDir: %q
http:
  hostname: "0.0.0.0"
  port: %d
log:
  level: debug
  json: false
proxyAccessLog: true
`, tsClientID, tsClientSecret, tsTags, dataDir, httpPort)
	}

	firstPort := getFreePort()
	proxy1 := startTSDProxyRawConfig(t, configForPort(firstPort), firstPort, tmpDir, dataDir)

	StartContainer(t, ContainerConfig{
		Labels: map[string]string{
			"tsdproxy.enable":    "true",
			"tsdproxy.name":      hostname,
			"tsdproxy.port.http": "80/http:80/http",
		},
	})

	client := NewTSNetClient(t, authKey)
	proxyURL := client.ProxyHTTPURL(hostname)
	WaitForProxyReachable(t, ctx, client, proxyURL, 120*time.Second)
	VerifyHTTPResponse(t, ctx, client, proxyURL, "Welcome to nginx!")

	stateDir := filepath.Join(dataDir, "default", hostname)
	require.DirExists(t, stateDir, "expected tsnet state dir for %s", hostname)

	proxy1.Stop(t)
	require.NoError(t, os.RemoveAll(stateDir), "failed to remove tsnet state dir %s", stateDir)

	secondPort := getFreePort()
	_ = startTSDProxyRawConfig(t, configForPort(secondPort), secondPort, tmpDir, dataDir)

	WaitForProxyReachable(t, ctx, client, proxyURL, 120*time.Second)
	VerifyHTTPResponse(t, ctx, client, proxyURL, "Welcome to nginx!")
}
