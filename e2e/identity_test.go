// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWhoAmIEndpoint(t *testing.T) {
	requireTailscaleAuth(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	proxy := StartTSDProxy(t, TSDProxyConfig{
		AdminAllowLocalhost: true,
	})

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, proxy.BaseURL+"/api/whoami", nil)
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode,
		"whoami from localhost should return 401 (no Tailscale identity): body=%s", string(body))
}

func TestIdentityHeadersInjected(t *testing.T) {
	authKey := requireTailscaleAuth(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	StartTSDProxy(t, TSDProxyConfig{AuthKey: authKey})

	hostname := fmt.Sprintf("e2e-identity-%d", time.Now().UnixNano())
	StartContainer(t, ContainerConfig{Labels: httpLabels(hostname)})

	client := NewTSNetClient(t, authKey)
	proxyURL := client.ProxyHTTPURL(hostname)
	WaitForProxyReachable(t, ctx, client, proxyURL, 60*time.Second)

	resp, err := client.Get(ctx, proxyURL)
	require.NoError(t, err)
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)

	assert.Equal(t, http.StatusOK, resp.StatusCode)
}
