// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"testing"
	"time"

	ctypes "github.com/moby/moby/api/types/container"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

func TestBridgeNetworkDefault(t *testing.T) {
	authKey := requireTailscaleAuth(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	proxy := StartTSDProxy(t, TSDProxyConfig{
  AuthKey: authKey,
  Tags: tsTags,
})
	t.Logf("tsdproxy started on port %d", proxy.HTTPPort)

	hostname := fmt.Sprintf("e2e-bridge-default-%d", time.Now().UnixNano())

	StartContainer(t, ContainerConfig{
		Labels: map[string]string{
			"tsdproxy.enable":     "true",
			"tsdproxy.ephemeral":  "true",
			"tsdproxy.name":       hostname,
			"tsdproxy.port.web":   "443:80/http",
			"tsdproxy.port.http":  "80/http:80/http",
		},
		ExposedPorts: []string{"80/tcp"},
	})

	client := NewTSNetClient(t, authKey)
	proxyURL := client.ProxyHTTPURL(hostname)

	WaitForProxyReachable(t, ctx, client, proxyURL, 90*time.Second)
	VerifyHTTPResponse(t, ctx, client, proxyURL, "Welcome to nginx!")
}

func TestAutoDetectEnabled(t *testing.T) {
	authKey := requireTailscaleAuth(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	proxy := StartTSDProxy(t, TSDProxyConfig{
  AuthKey: authKey,
  Tags: tsTags,
})
	t.Logf("tsdproxy started on port %d", proxy.HTTPPort)

	hostname := fmt.Sprintf("e2e-autodetect-%d", time.Now().UnixNano())

	StartContainer(t, ContainerConfig{
		Labels: map[string]string{
			"tsdproxy.enable":     "true",
			"tsdproxy.ephemeral":  "true",
			"tsdproxy.name":       hostname,
			"tsdproxy.autodetect": "true",
			"tsdproxy.port.web":   "443:80/http",
			"tsdproxy.port.http":  "80/http:80/http",
		},
		ExposedPorts: []string{"80/tcp"},
	})

	client := NewTSNetClient(t, authKey)
	proxyURL := client.ProxyHTTPURL(hostname)

	WaitForProxyReachable(t, ctx, client, proxyURL, 120*time.Second)
	VerifyHTTPResponse(t, ctx, client, proxyURL, "Welcome to nginx!")
}

func TestHostNetworkMode(t *testing.T) {
	// Probe: verify that the Docker daemon supports host networking.
	// Docker Desktop on macOS and some CI environments do not support it.
	probeCtx, probeCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer probeCancel()

	probeC, err := testcontainers.Run(probeCtx, "nginxinc/nginx-unprivileged:latest",
		testcontainers.CustomizeRequest(testcontainers.GenericContainerRequest{
			ContainerRequest: testcontainers.ContainerRequest{
				Image:       "nginxinc/nginx-unprivileged:latest",
				NetworkMode: ctypes.NetworkMode("host"),
			},
			Started: true,
		}),
		testcontainers.WithWaitStrategy(
			wait.ForListeningPort("8080/tcp").WithStartupTimeout(15*time.Second),
		),
	)
	if err != nil {
		t.Skip(fmt.Sprintf("host networking not available: %v", err))
	}
	// Clean up probe container.
	probeC.Terminate(probeCtx)

	// Host networking is available — run the full test.
	authKey := requireTailscaleAuth(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	proxy := StartTSDProxy(t, TSDProxyConfig{
  AuthKey:        authKey,
  		Tags:           tsTags,
  TargetHostname: "127.0.0.1",
})
	t.Logf("tsdproxy started on port %d", proxy.HTTPPort)

	hostname := fmt.Sprintf("e2e-host-net-%d", time.Now().UnixNano())

	StartContainer(t, ContainerConfig{
		Image:       "nginxinc/nginx-unprivileged:latest",
		NetworkMode: "host",
		Labels: map[string]string{
			"tsdproxy.enable":    "true",
			"tsdproxy.ephemeral": "true",
			"tsdproxy.name":      hostname,
			"tsdproxy.port.http": "80/http:8080/http",
		},
		WaitPort: "8080/tcp",
	})

	client := NewTSNetClient(t, authKey)
	proxyURL := client.ProxyHTTPURL(hostname)

	WaitForProxyReachable(t, ctx, client, proxyURL, 90*time.Second)
	VerifyHTTPResponse(t, ctx, client, proxyURL, "Welcome to nginx!")
}

func TestCustomTargetHostname(t *testing.T) {
	authKey := requireTailscaleAuth(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	proxy := StartTSDProxy(t, TSDProxyConfig{
  AuthKey:        authKey,
  		Tags:           tsTags,
  TargetHostname: "172.17.0.1",
})
	t.Logf("tsdproxy started on port %d with TargetHostname=172.17.0.1", proxy.HTTPPort)

	hostname := fmt.Sprintf("e2e-custom-host-%d", time.Now().UnixNano())

	StartContainer(t, ContainerConfig{
		Labels: map[string]string{
			"tsdproxy.enable":    "true",
			"tsdproxy.ephemeral": "true",
			"tsdproxy.name":      hostname,
			"tsdproxy.port.web":  "443:80/http",
			"tsdproxy.port.http": "80/http:80/http",
		},
		ExposedPorts: []string{"80/tcp"},
	})

	client := NewTSNetClient(t, authKey)
	proxyURL := client.ProxyHTTPURL(hostname)

	WaitForProxyReachable(t, ctx, client, proxyURL, 90*time.Second)
	VerifyHTTPResponse(t, ctx, client, proxyURL, "Welcome to nginx!")
}
