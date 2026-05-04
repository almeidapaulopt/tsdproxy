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

func TestMultiProviderSelection(t *testing.T) {
	requireOAuth(t)
	authKey := requireTailscaleAuth(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	httpPort := getFreePort()
	tmpDir := e2eTestDataDir(t)
	dataDir := filepath.Join(tmpDir, "data")
	require.NoError(t, os.MkdirAll(dataDir, 0o755))

	// Two providers using OAuth to avoid single-use auth key conflicts.
	configContent := fmt.Sprintf(`defaultProxyProvider: default
docker:
  local:
    targetHostname: "172.17.0.1"
tailscale:
  providers:
    default:
      clientId: %q
      clientSecret: %q
      tags: %q
    alternative:
      clientId: %q
      clientSecret: %q
      tags: %q
  dataDir: %q
http:
  hostname: "0.0.0.0"
  port: %d
log:
  level: debug
  json: false
proxyAccessLog: true
`, tsClientID, tsClientSecret, tsTags, tsClientID, tsClientSecret, tsTags, dataDir, httpPort)

	proxy := startTSDProxyRawConfig(t, configContent, httpPort, tmpDir, dataDir)

	// Container using "alternative" provider.
	hostnameAlt := fmt.Sprintf("e2e-alt-prov-%d", time.Now().UnixNano())
	StartContainer(t, ContainerConfig{
		Labels: map[string]string{
			"tsdproxy.enable":        "true",
			"tsdproxy.ephemeral":     "true",
			"tsdproxy.name":          hostnameAlt,
			"tsdproxy.proxyprovider": "alternative",
			"tsdproxy.port.http":     "80/http:80/http",
		},
	})

	// Container using default provider (no proxyprovider label).
	hostnameDef := fmt.Sprintf("e2e-def-prov-%d", time.Now().UnixNano())
	StartContainer(t, ContainerConfig{
		Labels: map[string]string{
			"tsdproxy.enable":    "true",
			"tsdproxy.ephemeral": "true",
			"tsdproxy.name":      hostnameDef,
			"tsdproxy.port.http": "80/http:80/http",
		},
	})

	client := NewTSNetClient(t, authKey)

	urlAlt := client.ProxyHTTPURL(hostnameAlt)
	urlDef := client.ProxyHTTPURL(hostnameDef)

	WaitForProxyReachable(t, ctx, client, urlAlt, 90*time.Second)
	VerifyHTTPResponse(t, ctx, client, urlAlt, "Welcome to nginx!")

	WaitForProxyReachable(t, ctx, client, urlDef, 90*time.Second)
	VerifyHTTPResponse(t, ctx, client, urlDef, "Welcome to nginx!")

	// Verify state dirs exist under the correct provider subdirectory.
	altStateDir := filepath.Join(dataDir, "alternative", hostnameAlt)
	defStateDir := filepath.Join(dataDir, "default", hostnameDef)
	require.DirExists(t, altStateDir, "alternative provider state dir should exist")
	require.DirExists(t, defStateDir, "default provider state dir should exist")
	_ = proxy
}
