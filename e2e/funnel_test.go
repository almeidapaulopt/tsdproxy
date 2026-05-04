// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestModernFunnelPortOption(t *testing.T) {
	authKey := requireTailscaleAuth(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "modern funnel OK")
	}))
	t.Cleanup(backend.Close)

	backendPort, err := strconv.Atoi(mustURLPort(t, backend.URL))
	require.NoError(t, err, "failed to parse backend port")

	_ = StartTSDProxy(t, TSDProxyConfig{
  AuthKey:        authKey,
  TargetHostname: "127.0.0.1",
})

	hostname := fmt.Sprintf("modern-funnel-%d", time.Now().UnixNano())

	StartContainer(t, ContainerConfig{
		Image:    "alpine",
		Cmd:      []string{"sleep", "3600"},
		WaitPort: "skip",
		Labels: map[string]string{
			"tsdproxy.enable":    "true",
			"tsdproxy.ephemeral": "true",
			"tsdproxy.name":      hostname,
			"tsdproxy.port.http": fmt.Sprintf("80/http:%d/http, tailscale_funnel", backendPort),
			"tsdproxy.scheme":    "http",
		},
	})

	client := NewTSNetClient(t, authKey)

	// Check funnel capability on the peer.
	peer := waitForPeerByDNSName(t, ctx, client, hostname, 90*time.Second)
	t.Logf("peer %s capabilities: %v", hostname, peer.Capabilities)
	hasFunnel := false
	for _, cap := range peer.Capabilities {
		if string(cap) == "funnel" {
			hasFunnel = true
			break
		}
	}
	if !hasFunnel {
		t.Skipf("funnel capability not present on peer %s (capabilities: %v). "+
			"This test requires funnel to be enabled in the tailnet ACL policy.", hostname, peer.Capabilities)
	}

	proxyURL := client.ProxyURL(hostname)
	WaitForProxyReachable(t, ctx, client, proxyURL, 90*time.Second)
	VerifyHTTPResponse(t, ctx, client, proxyURL, "modern funnel OK")
}
