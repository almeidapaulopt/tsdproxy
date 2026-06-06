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

func TestAdminEndpointRejectsUnauthenticated(t *testing.T) {
	requireTailscaleAuth(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	proxy := StartTSDProxy(t, TSDProxyConfig{})

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, proxy.BaseURL+"/api/v1/proxies", nil)
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)

	assert.True(t, resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden,
		"unauthenticated request should return 401 or 403, got %d", resp.StatusCode)
}

func TestAdminAllowLocalhost(t *testing.T) {
	authKey := requireTailscaleAuth(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	proxy := StartTSDProxy(t, TSDProxyConfig{
		AuthKey:             authKey,
		AdminAllowLocalhost: true,
	})

	hostname := fmt.Sprintf("e2e-auth-localhost-%d", time.Now().UnixNano())
	StartContainer(t, ContainerConfig{Labels: httpLabels(hostname)})

	client := NewTSNetClient(t, authKey)
	WaitForProxyReachable(t, ctx, client, client.ProxyHTTPURL(hostname), 60*time.Second)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, proxy.BaseURL+"/api/v1/proxies", nil)
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode,
		"localhost with adminAllowLocalhost should get 200, got %d", resp.StatusCode)
}

func TestAdminEndpointRequiresAuthWithoutLocalhost(t *testing.T) {
	authKey := requireTailscaleAuth(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	proxy := StartTSDProxy(t, TSDProxyConfig{
		AuthKey: authKey,
	})

	hostname := fmt.Sprintf("e2e-auth-nolocalhost-%d", time.Now().UnixNano())
	StartContainer(t, ContainerConfig{Labels: httpLabels(hostname)})

	client := NewTSNetClient(t, authKey)
	WaitForProxyReachable(t, ctx, client, client.ProxyHTTPURL(hostname), 60*time.Second)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, proxy.BaseURL+"/api/version", nil)
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)

	assert.True(t, resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden,
		"request without adminAllowLocalhost should return 401 or 403, got %d", resp.StatusCode)
}
