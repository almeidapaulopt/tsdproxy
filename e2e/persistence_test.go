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

func TestPersistentIdentityAcrossRestart(t *testing.T) {
	authKey := requireTailscaleAuth(t)
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	tmpDir := e2eTestDataDir(t)
	dataDir := filepath.Join(tmpDir, "data")
	require.NoError(t, os.MkdirAll(dataDir, 0o755))

	hostname := fmt.Sprintf("e2e-persist-%d", time.Now().UnixNano())

	makeConfig := func(httpPort int) string {
		return generateConfig(configParams{
			HTTPPort: httpPort,
			AuthKey:  authKey,
			DataDir:  dataDir,
		})
	}

	// First instance.
	firstPort := getFreePort()
	proxy1 := startTSDProxyRawConfig(t, makeConfig(firstPort), firstPort, tmpDir, dataDir)

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

	// Capture node ID before restart.
	peer1, err := client.GetPeerByDNSName(ctx, hostname)
	require.NoError(t, err, "failed to get peer before restart")
	nodeID1 := peer1.ID
	t.Logf("first instance node ID: %s", nodeID1)

	// Stop first instance.
	proxy1.Stop(t)

	// Restart with same data dir, different port.
	secondPort := getFreePort()
	proxy2 := startTSDProxyRawConfig(t, makeConfig(secondPort), secondPort, tmpDir, dataDir)

	WaitForProxyReachable(t, ctx, client, proxyURL, 120*time.Second)
	VerifyHTTPResponse(t, ctx, client, proxyURL, "Welcome to nginx!")

	// Verify same node ID.
	peer2, err := client.GetPeerByDNSName(ctx, hostname)
	require.NoError(t, err, "failed to get peer after restart")
	nodeID2 := peer2.ID
	t.Logf("second instance node ID: %s", nodeID2)

	require.Equal(t, nodeID1, nodeID2,
		"node ID should persist across restarts")
	_ = proxy2
}
