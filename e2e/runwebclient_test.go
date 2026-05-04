// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

//go:build e2e

package e2e

import (
	"bufio"
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRunWebClientLabel(t *testing.T) {
	authKey := requireTailscaleAuth(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	_ = StartTSDProxy(t, TSDProxyConfig{
  AuthKey: authKey,
})

	hostname := fmt.Sprintf("e2e-runwebclient-%d", time.Now().UnixNano())

	StartContainer(t, ContainerConfig{
		Labels: map[string]string{
			"tsdproxy.enable":       "true",
			"tsdproxy.ephemeral":    "true",
			"tsdproxy.name":         hostname,
			"tsdproxy.runwebclient": "true",
			"tsdproxy.port.http":    "80/http:80/http",
		},
	})

	client := NewTSNetClient(t, authKey)
	proxyURL := client.ProxyHTTPURL(hostname)

	WaitForProxyReachable(t, ctx, client, proxyURL, 90*time.Second)
	VerifyHTTPResponse(t, ctx, client, proxyURL, "Welcome to nginx!")

	webClientAddr := fmt.Sprintf("%s.%s:5252", hostname, client.MagicDNSSuffix)
	conn, err := client.Dial(ctx, "tcp", webClientAddr)
	require.NoError(t, err, "failed to dial Tailscale web client port 5252")
	defer conn.Close()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+webClientAddr, nil)
	require.NoError(t, err)
	require.NoError(t, req.Write(conn), "failed to write HTTP request to web client")

	resp, err := http.ReadResponse(bufio.NewReader(conn), req)
	require.NoError(t, err, "failed to read web client response")
	defer resp.Body.Close()

	require.True(t, resp.StatusCode >= 200 && resp.StatusCode < 400,
		"expected successful response or redirect from Tailscale web client on port 5252, got %d", resp.StatusCode)
}
