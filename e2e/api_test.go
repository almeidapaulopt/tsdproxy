// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

//go:build e2e

package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type listProxiesResponse struct {
	Proxies []apiProxyItem `json:"proxies"`
}

type apiProxyItem struct {
	Name           string `json:"name"`
	Status         string `json:"status"`
	URL            string `json:"url"`
	Label          string `json:"label"`
	TargetProvider string `json:"targetProvider"`
}

type apiVersionResponse struct {
	Version string `json:"version"`
	Author  string `json:"author"`
	IsDirty bool   `json:"isDirty"`
}

type apiHealthResponse struct {
	Total   int `json:"total"`
	Running int `json:"running"`
	Stopped int `json:"stopped"`
	Error   int `json:"error"`
}

func apiGet(t *testing.T, ctx context.Context, baseURL, path string) []byte {
	t.Helper()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+path, nil)
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode, "GET %s: body=%s", path, string(body))
	return body
}

func TestAPIListProxies(t *testing.T) {
	authKey := requireTailscaleAuth(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	proxy := StartTSDProxy(t, TSDProxyConfig{
		AuthKey:             authKey,
		AdminAllowLocalhost: true,
	})

	hostname := fmt.Sprintf("e2e-api-list-%d", time.Now().UnixNano())
	StartContainer(t, ContainerConfig{Labels: httpLabels(hostname)})

	client := NewTSNetClient(t, authKey)
	proxyURL := client.ProxyHTTPURL(hostname)
	WaitForProxyReachable(t, ctx, client, proxyURL, 90*time.Second)

	require.Eventually(t, func() bool {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, proxy.BaseURL+"/api/v1/proxies", nil)
		if err != nil {
			return false
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return false
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return false
		}
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return false
		}
		var listResp listProxiesResponse
		if err := json.Unmarshal(body, &listResp); err != nil {
			return false
		}
		for _, p := range listResp.Proxies {
			if p.Name == hostname {
				return true
			}
		}
		return false
	}, 60*time.Second, 2*time.Second,
		"proxy %q should eventually appear in API list", hostname)
}

func TestAPIGetProxy(t *testing.T) {
	authKey := requireTailscaleAuth(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	proxy := StartTSDProxy(t, TSDProxyConfig{
		AuthKey:             authKey,
		AdminAllowLocalhost: true,
	})

	hostname := fmt.Sprintf("e2e-api-get-%d", time.Now().UnixNano())
	StartContainer(t, ContainerConfig{Labels: httpLabels(hostname)})

	client := NewTSNetClient(t, authKey)
	proxyURL := client.ProxyHTTPURL(hostname)
	WaitForProxyReachable(t, ctx, client, proxyURL, 90*time.Second)

	path := fmt.Sprintf("/api/v1/proxies/%s", hostname)

	require.Eventually(t, func() bool {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, proxy.BaseURL+path, nil)
		require.NoError(t, err)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return false
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return false
		}
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return false
		}
		var p apiProxyItem
		if err := json.Unmarshal(body, &p); err != nil {
			return false
		}
		return p.Name == hostname && p.URL != ""
	}, 60*time.Second, 2*time.Second,
		"proxy %q should eventually be retrievable via API", hostname)
}

func TestAPIVersion(t *testing.T) {
	requireTailscaleAuth(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	proxy := StartTSDProxy(t, TSDProxyConfig{
		AdminAllowLocalhost: true,
	})

	body := apiGet(t, ctx, proxy.BaseURL, "/api/version")

	var v apiVersionResponse
	require.NoError(t, json.Unmarshal(body, &v))
	assert.NotEmpty(t, v.Version)
	assert.NotEmpty(t, v.Author)
}

func TestAPIHealth(t *testing.T) {
	authKey := requireTailscaleAuth(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	proxy := StartTSDProxy(t, TSDProxyConfig{
		AuthKey:             authKey,
		AdminAllowLocalhost: true,
	})

	hostname := fmt.Sprintf("e2e-api-health-%d", time.Now().UnixNano())
	StartContainer(t, ContainerConfig{Labels: httpLabels(hostname)})

	client := NewTSNetClient(t, authKey)
	proxyURL := client.ProxyHTTPURL(hostname)
	WaitForProxyReachable(t, ctx, client, proxyURL, 90*time.Second)

	require.Eventually(t, func() bool {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, proxy.BaseURL+"/api/health", nil)
		if err != nil {
			return false
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return false
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return false
		}
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return false
		}
		var h apiHealthResponse
		if err := json.Unmarshal(body, &h); err != nil {
			return false
		}
		return h.Total >= 1 && h.Running >= 1
	}, 30*time.Second, 2*time.Second,
		"health endpoint should eventually show at least 1 running proxy")
}
