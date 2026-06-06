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
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func postDashboardAction(t *testing.T, ctx context.Context, baseURL, path string) int {
	t.Helper()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+path, nil)
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	return resp.StatusCode
}

func TestPauseResumeProxy(t *testing.T) {
	authKey := requireTailscaleAuth(t)
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	proxy := StartTSDProxy(t, TSDProxyConfig{
		AuthKey:             authKey,
		AdminAllowLocalhost: true,
	})

	hostname := fmt.Sprintf("e2e-pause-%d", time.Now().UnixNano())
	StartContainer(t, ContainerConfig{Labels: httpLabels(hostname)})

	client := NewTSNetClient(t, authKey)
	proxyURL := client.ProxyHTTPURL(hostname)
	WaitForProxyReachable(t, ctx, client, proxyURL, 90*time.Second)
	VerifyHTTPResponse(t, ctx, client, proxyURL, "Welcome to nginx!")

	statusDeadlin := time.Now().Add(120 * time.Second)
	var statusOK bool
	for time.Now().Before(statusDeadlin) {
		apiCtx, apiCancel := context.WithTimeout(context.Background(), 5*time.Second)
		req, _ := http.NewRequestWithContext(apiCtx, http.MethodGet, proxy.BaseURL+"/api/v1/proxies/"+hostname, nil)
		resp, err := http.DefaultClient.Do(req)
		apiCancel()
		if err != nil {
			time.Sleep(2 * time.Second)
			continue
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			time.Sleep(2 * time.Second)
			continue
		}
		var result map[string]any
		if err := json.Unmarshal(body, &result); err != nil {
			time.Sleep(2 * time.Second)
			continue
		}
		s, _ := result["status"].(string)
		if strings.EqualFold(s, "running") {
			statusOK = true
			break
		}
		time.Sleep(2 * time.Second)
	}
	require.True(t, statusOK, "proxy %s status should become running within 120s", hostname)

	pausePath := fmt.Sprintf("/api/proxy/%s/pause", hostname)
	statusCode := postDashboardAction(t, ctx, proxy.BaseURL, pausePath)
	require.Equal(t, http.StatusOK, statusCode, "pause should return 200")

	assert.Eventually(t, func() bool {
		verifyCtx, verifyCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer verifyCancel()
		resp, err := client.Get(verifyCtx, proxyURL)
		if err != nil {
			return true
		}
		resp.Body.Close()
		return false
	}, 45*time.Second, 2*time.Second, "proxy should become unreachable after pause")

	resumePath := fmt.Sprintf("/api/proxy/%s/resume", hostname)
	statusCode = postDashboardAction(t, ctx, proxy.BaseURL, resumePath)
	require.Equal(t, http.StatusOK, statusCode, "resume should return 200")
}

func TestRestartProxy(t *testing.T) {
	authKey := requireTailscaleAuth(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	proxy := StartTSDProxy(t, TSDProxyConfig{
		AuthKey:             authKey,
		AdminAllowLocalhost: true,
	})

	hostname := fmt.Sprintf("e2e-restart-%d", time.Now().UnixNano())
	StartContainer(t, ContainerConfig{Labels: httpLabels(hostname)})

	client := NewTSNetClient(t, authKey)
	proxyURL := client.ProxyHTTPURL(hostname)
	WaitForProxyReachable(t, ctx, client, proxyURL, 90*time.Second)
	VerifyHTTPResponse(t, ctx, client, proxyURL, "Welcome to nginx!")

	restartPath := fmt.Sprintf("/api/proxy/%s/restart", hostname)
	statusCode := postDashboardAction(t, ctx, proxy.BaseURL, restartPath)
	require.Equal(t, http.StatusOK, statusCode, "restart should return 200")

	WaitForProxyReachable(t, ctx, client, proxyURL, 90*time.Second)
	VerifyHTTPResponse(t, ctx, client, proxyURL, "Welcome to nginx!")
}

func TestPinToggleProxy(t *testing.T) {
	authKey := requireTailscaleAuth(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	proxy := StartTSDProxy(t, TSDProxyConfig{
		AuthKey:             authKey,
		AdminAllowLocalhost: true,
	})

	hostname := fmt.Sprintf("e2e-pin-%d", time.Now().UnixNano())
	StartContainer(t, ContainerConfig{Labels: httpLabels(hostname)})

	client := NewTSNetClient(t, authKey)
	proxyURL := client.ProxyHTTPURL(hostname)
	WaitForProxyReachable(t, ctx, client, proxyURL, 90*time.Second)

	pinPath := fmt.Sprintf("/api/dashboard/pin/%s", hostname)

	statusCode := postDashboardAction(t, ctx, proxy.BaseURL, pinPath)
	require.Equal(t, http.StatusOK, statusCode, "pin toggle (on) should return 200")

	statusCode = postDashboardAction(t, ctx, proxy.BaseURL, pinPath)
	require.Equal(t, http.StatusOK, statusCode, "pin toggle (off) should return 200")
}
