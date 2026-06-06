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

func TestMetricsEndpoint(t *testing.T) {
	authKey := requireTailscaleAuth(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	proxy := StartTSDProxy(t, TSDProxyConfig{
		AuthKey:             authKey,
		AdminAllowLocalhost: true,
	})

	hostname := "e2e-metrics-" + fmt.Sprintf("%d", time.Now().UnixNano())
	StartContainer(t, ContainerConfig{Labels: httpLabels(hostname)})

	client := NewTSNetClient(t, authKey)
	WaitForProxyReachable(t, ctx, client, client.ProxyHTTPURL(hostname), 60*time.Second)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, proxy.BaseURL+"/metrics", nil)
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode, "metrics endpoint should return 200: body=%s", string(body))

	assert.Contains(t, string(body), "tsdproxy_proxy_status",
		"metrics should contain tsdproxy_proxy_status metric")
}
