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

// httpLabels returns Docker labels for a container that should be proxied
// via HTTP (no TLS, avoids ACME rate limits in CI).
func httpLabels(hostname string) map[string]string {
	return map[string]string{
		"tsdproxy.enable":    "true",
		"tsdproxy.name":      hostname,
		"tsdproxy.port.http": "80/http:80/http",
	}
}

func TestBasicProxy(t *testing.T) {
	authKey := requireTailscaleAuth(t)
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	proxy := StartTSDProxy(t, TSDProxyConfig{AuthKey: authKey})
	t.Logf("tsdproxy started on port %d", proxy.HTTPPort)

	hostname := fmt.Sprintf("e2e-basic-%d", time.Now().UnixNano())
	StartContainer(t, ContainerConfig{Labels: httpLabels(hostname)})

	client := NewTSNetClient(t, authKey)
	proxyURL := client.ProxyHTTPURL(hostname)

	WaitForProxyReachable(t, ctx, client, proxyURL, 150*time.Second)
	VerifyHTTPResponse(t, ctx, client, proxyURL, "Welcome to nginx!")
}

func TestCustomHostname(t *testing.T) {
	authKey := requireTailscaleAuth(t)
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	proxy := StartTSDProxy(t, TSDProxyConfig{AuthKey: authKey})
	t.Logf("tsdproxy started on port %d", proxy.HTTPPort)

	hostname := fmt.Sprintf("e2e-custom-%d", time.Now().UnixNano())
	StartContainer(t, ContainerConfig{Labels: httpLabels(hostname)})

	client := NewTSNetClient(t, authKey)
	proxyURL := client.ProxyHTTPURL(hostname)

	WaitForProxyReachable(t, ctx, client, proxyURL, 120*time.Second)
	VerifyHTTPResponse(t, ctx, client, proxyURL, "Welcome to nginx!")
}

func TestContainerStopRemovesProxy(t *testing.T) {
	authKey := requireTailscaleAuth(t)
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	proxy := StartTSDProxy(t, TSDProxyConfig{AuthKey: authKey})
	t.Logf("tsdproxy started on port %d", proxy.HTTPPort)

	hostname := fmt.Sprintf("e2e-stop-%d", time.Now().UnixNano())
	ctr := StartContainer(t, ContainerConfig{Labels: httpLabels(hostname)})

	client := NewTSNetClient(t, authKey)
	proxyURL := client.ProxyHTTPURL(hostname)

	WaitForProxyReachable(t, ctx, client, proxyURL, 120*time.Second)
	VerifyHTTPResponse(t, ctx, client, proxyURL, "Welcome to nginx!")

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer stopCancel()
	require.NoError(t, ctr.Stop(stopCtx, nil), "failed to stop container")

	assert.Eventually(t, func() bool {
		verifyCtx, verifyCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer verifyCancel()
		resp, err := client.Get(verifyCtx, proxyURL)
		if err != nil {
			return true
		}
		resp.Body.Close()
		return false
	}, 45*time.Second, 2*time.Second, "expected proxy to become unreachable after container stop")
}

func TestMultipleContainers(t *testing.T) {
	authKey := requireTailscaleAuth(t)
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	proxy := StartTSDProxy(t, TSDProxyConfig{AuthKey: authKey})
	t.Logf("tsdproxy started on port %d", proxy.HTTPPort)

	suffix := time.Now().UnixNano()
	hostname1 := fmt.Sprintf("e2e-multi-1-%d", suffix)
	hostname2 := fmt.Sprintf("e2e-multi-2-%d", suffix)

	StartContainer(t, ContainerConfig{Labels: httpLabels(hostname1)})
	StartContainer(t, ContainerConfig{Labels: httpLabels(hostname2)})

	client := NewTSNetClient(t, authKey)

	url1 := client.ProxyHTTPURL(hostname1)
	url2 := client.ProxyHTTPURL(hostname2)

	WaitForProxyReachable(t, ctx, client, url1, 120*time.Second)
	VerifyHTTPResponse(t, ctx, client, url1, "Welcome to nginx!")

	WaitForProxyReachable(t, ctx, client, url2, 120*time.Second)
	VerifyHTTPResponse(t, ctx, client, url2, "Welcome to nginx!")
}

func TestEphemeralProxy(t *testing.T) {
	authKey := requireTailscaleAuth(t)
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	proxy := StartTSDProxy(t, TSDProxyConfig{AuthKey: authKey})
	t.Logf("tsdproxy started on port %d", proxy.HTTPPort)

	hostname := fmt.Sprintf("e2e-eph-%d", time.Now().UnixNano())
	ctr := StartContainer(t, ContainerConfig{
		Labels: map[string]string{
			"tsdproxy.enable":    "true",
			"tsdproxy.name":      hostname,
			"tsdproxy.port.http": "80/http:80/http",
			"tsdproxy.ephemeral": "true",
		},
	})

	client := NewTSNetClient(t, authKey)
	proxyURL := client.ProxyHTTPURL(hostname)
	stateDir := filepath.Join(proxy.DataDir, "default", hostname)

	WaitForProxyReachable(t, ctx, client, proxyURL, 120*time.Second)
	VerifyHTTPResponse(t, ctx, client, proxyURL, "Welcome to nginx!")
	require.DirExists(t, stateDir, "expected ephemeral proxy state dir to exist while proxy is running")

	peer, err := client.GetPeerByDNSName(ctx, hostname)
	require.NoError(t, err, "expected to find peer for ephemeral proxy %s", hostname)
	assert.True(t, peer.Online, "ephemeral peer should be online")

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer stopCancel()
	require.NoError(t, ctr.Stop(stopCtx, nil), "failed to stop container")

	assert.Eventually(t, func() bool {
		checkCtx, checkCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer checkCancel()
		_, err := client.GetPeerByDNSName(checkCtx, hostname)
		return err != nil
	}, 45*time.Second, 2*time.Second, "expected ephemeral peer %s to disappear from tailnet after container stop", hostname)

	assert.Eventually(t, func() bool {
		_, err := os.Stat(stateDir)
		return os.IsNotExist(err)
	}, 45*time.Second, 2*time.Second, "expected ephemeral proxy state dir %s to be removed after stop", stateDir)
}

func TestContainerRestart(t *testing.T) {
	authKey := requireTailscaleAuth(t)
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	proxy := StartTSDProxy(t, TSDProxyConfig{AuthKey: authKey})
	t.Logf("tsdproxy started on port %d", proxy.HTTPPort)

	hostname := fmt.Sprintf("e2e-restart-%d", time.Now().UnixNano())

	ctr := StartContainer(t, ContainerConfig{Labels: httpLabels(hostname)})

	client := NewTSNetClient(t, authKey)
	proxyURL := client.ProxyHTTPURL(hostname)

	WaitForProxyReachable(t, ctx, client, proxyURL, 120*time.Second)
	VerifyHTTPResponse(t, ctx, client, proxyURL, "Welcome to nginx!")

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 10*time.Second)
	require.NoError(t, ctr.Stop(stopCtx, nil), "failed to stop first container")
	stopCancel()

	assert.Eventually(t, func() bool {
		verifyCtx, verifyCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer verifyCancel()
		resp, err := client.Get(verifyCtx, proxyURL)
		if err != nil {
			return true
		}
		resp.Body.Close()
		return false
	}, 45*time.Second, 2*time.Second, "expected proxy to become unreachable after container stop")

	StartContainer(t, ContainerConfig{Labels: httpLabels(hostname)})

	WaitForProxyReachable(t, ctx, client, proxyURL, 120*time.Second)
	VerifyHTTPResponse(t, ctx, client, proxyURL, "Welcome to nginx!")
}
