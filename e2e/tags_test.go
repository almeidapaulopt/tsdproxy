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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestListTagsPropagation(t *testing.T) {
	requireOAuth(t)
	authKey := requireTailscaleAuth(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	hostname := fmt.Sprintf("e2e-tags-prop-%d", time.Now().UnixNano())

	listPath := filepath.Join(e2eTestDataDir(t), "tags-list.yaml")
	WriteListProviderFile(t, listPath, map[string]ListEntry{
		hostname: {
			ProxyProvider: "default",
			Tailscale: ListTailscale{
				Tags: tsTags,
			},
			Dashboard: ListDashboard{Visible: true, Label: "Tags Test"},
			Ports: map[string]ListPort{
				"80/http": {
					Targets: []string{"http://127.0.0.1:1"}, // unreachable, just need node to register
				},
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
  tagslist:
    filename: %q
tailscale:
  providers:
    default:
      clientId: %q
      clientSecret: %q
  dataDir: %q
http:
  hostname: "0.0.0.0"
  port: %d
log:
  level: debug
  json: false
proxyAccessLog: true
`, listPath, tsClientID, tsClientSecret, dataDir, httpPort)

	proxy := startTSDProxyRawConfig(t, configContent, httpPort, tmpDir, dataDir)

	client := NewTSNetClient(t, authKey)

	// Wait for the peer to appear (target is unreachable but node should register).
	peer := waitForPeerByDNSName(t, ctx, client, hostname, 90*time.Second)

	// Verify tags are present.
	require.NotNil(t, peer.Tags, "peer should have tags set")
	assert.Contains(t, peer.Tags.AsSlice(), tsTags,
		"expected tag %s on node %s, got %v", tsTags, hostname, peer.Tags.AsSlice())
	_ = proxy
}
