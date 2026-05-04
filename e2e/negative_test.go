// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	negativeStartupTimeout = 90 * time.Second
	negativePollInterval   = 2 * time.Second
	negativeWaitTimeout    = 15 * time.Second
)

func TestContainerWithoutEnableLabel(t *testing.T) {
	authKey := requireTailscaleAuth(t)
	ctx, cancel := context.WithTimeout(context.Background(), negativeStartupTimeout)
	defer cancel()

	proxy := StartTSDProxy(t, TSDProxyConfig{
		AuthKey: authKey,
	})
	t.Logf("tsdproxy started on port %d", proxy.HTTPPort)

	hostname := fmt.Sprintf("e2e-no-enable-%d", time.Now().UnixNano())
	StartContainer(t, ContainerConfig{
		Labels: map[string]string{
			"tsdproxy.name": hostname,
		},
	})

	client := NewTSNetClient(t, authKey)

	assert.Never(t, func() bool {
		proxyURL := client.ProxyHTTPURL(hostname)
		resp, err := client.Get(ctx, proxyURL)
		if err != nil {
			return false
		}
		resp.Body.Close()
		return true
	}, negativeWaitTimeout, negativePollInterval, "proxy should NOT be reachable for container without tsdproxy.enable=true")
}

func TestContainerWithEnableButNoPorts(t *testing.T) {
	authKey := requireTailscaleAuth(t)

	proxy := StartTSDProxy(t, TSDProxyConfig{
		AuthKey: authKey,
	})
	t.Logf("tsdproxy started on port %d", proxy.HTTPPort)

	hostname := fmt.Sprintf("e2e-no-ports-%d", time.Now().UnixNano())
	StartContainer(t, ContainerConfig{
		Image: "alpine",
		Cmd:   []string{"sleep", "3600"},
		Labels: map[string]string{
			"tsdproxy.enable": "true",
			"tsdproxy.name":   hostname,
		},
		WaitPort: "skip",
	})

	require.Eventually(t, func() bool {
		logContent := proxy.ReadLogFile(t)
		return strings.Contains(logContent, hostname)
	}, negativeWaitTimeout, negativePollInterval, "tsdproxy should log about the container with no ports")

	logContent := proxy.ReadLogFile(t)
	t.Logf("tsdproxy log tail:\n%s", lastNLines(logContent, 20))
}

func TestInvalidListTarget(t *testing.T) {
	authKey := requireTailscaleAuth(t)
	ctx, cancel := context.WithTimeout(context.Background(), negativeStartupTimeout)
	defer cancel()

	hostname := fmt.Sprintf("e2e-invalid-target-%d", time.Now().UnixNano())
	listFilePath := GenerateListProviderFile(t, map[string]ListEntry{
		hostname: {
			ProxyProvider: "default",
			Dashboard:     ListDashboard{Visible: true, Label: "Invalid Target Test"},
			Ports: map[string]ListPort{
				"80/http": {
					Targets: []string{"http://127.0.0.1:1"},
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
    targetHostname: "172.31.0.1"
lists:
  testlist:
    filename: %q
tailscale:
  providers:
    default:
      authKey: %q
  dataDir: %q
http:
  hostname: "0.0.0.0"
  port: %d
log:
  level: debug
  json: false
proxyAccessLog: true
`, listFilePath, authKey, dataDir, httpPort)

	proxy := startTSDProxyRawConfig(t, configContent, httpPort, tmpDir, dataDir)
	t.Logf("tsdproxy with invalid list target started on port %d", proxy.HTTPPort)

	require.Eventually(t, func() bool {
		logContent := proxy.ReadLogFile(t)
		return strings.Contains(logContent, hostname)
	}, negativeWaitTimeout, negativePollInterval, "tsdproxy should log about the invalid target entry")

	logContent := proxy.ReadLogFile(t)
	t.Logf("tsdproxy log tail:\n%s", lastNLines(logContent, 20))
	assert.Contains(t, logContent, hostname, "log should reference the invalid list target hostname")

	client := NewTSNetClient(t, authKey)
	proxyURL := client.ProxyHTTPURL(hostname)
	WaitForProxyReachable(t, ctx, client, proxyURL, negativeWaitTimeout)

	// WaitForProxyReachable returns on first non-error response but discards it.
	// Re-fetch to inspect the status code.
	resp, err := client.Get(ctx, proxyURL)
	require.NoError(t, err, "failed to GET %s", proxyURL)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusBadGateway, resp.StatusCode,
		"expected 502 Bad Gateway for invalid backend target, got %d", resp.StatusCode)
}

func TestMalformedPortLabel(t *testing.T) {
	authKey := requireTailscaleAuth(t)
	ctx, cancel := context.WithTimeout(context.Background(), negativeStartupTimeout)
	defer cancel()

	proxy := StartTSDProxy(t, TSDProxyConfig{
  AuthKey: authKey,
  Tags: tsTags,
})
	t.Logf("tsdproxy started on port %d", proxy.HTTPPort)

	hostname := fmt.Sprintf("e2e-malformed-port-%d", time.Now().UnixNano())
	StartContainer(t, ContainerConfig{
		Image: "alpine",
		Cmd:   []string{"sleep", "3600"},
		Labels: map[string]string{
			"tsdproxy.enable":    "true",
			"tsdproxy.ephemeral": "true",
			"tsdproxy.name":      hostname,
			"tsdproxy.port.http": "not-a-valid-port",
		},
		WaitPort: "skip",
	})

	client := NewTSNetClient(t, authKey)

	assert.Never(t, func() bool {
		proxyURL := client.ProxyHTTPURL(hostname)
		resp, err := client.Get(ctx, proxyURL)
		if err != nil {
			return false
		}
		resp.Body.Close()
		return true
	}, negativeWaitTimeout, negativePollInterval, "proxy should NOT be reachable for container with malformed port label")

	logContent := stripANSI(proxy.ReadLogFile(t))
	assert.Contains(t, logContent, hostname, "tsdproxy log should reference the container with malformed port label")
	t.Logf("tsdproxy log tail:\n%s", lastNLines(logContent, 20))
}

func TestDuplicateHostname(t *testing.T) {
	authKey := requireTailscaleAuth(t)
	ctx, cancel := context.WithTimeout(context.Background(), negativeStartupTimeout)
	defer cancel()

	proxy := StartTSDProxy(t, TSDProxyConfig{
  AuthKey: authKey,
  Tags: tsTags,
})
	t.Logf("tsdproxy started on port %d", proxy.HTTPPort)

	hostname := fmt.Sprintf("conflict-test-%d", time.Now().UnixNano())

	// Start two containers with the same tsdproxy.name but different images.
	StartContainer(t, ContainerConfig{
		Labels: map[string]string{
			"tsdproxy.enable":    "true",
			"tsdproxy.ephemeral": "true",
			"tsdproxy.name":      hostname,
			"tsdproxy.port.http": "80/http:80/http",
		},
	})

	StartContainer(t, ContainerConfig{
		Image: "nginxinc/nginx-unprivileged:latest",
		ExposedPorts: []string{"8080/tcp"},
		Labels: map[string]string{
			"tsdproxy.enable":    "true",
			"tsdproxy.ephemeral": "true",
			"tsdproxy.name":      hostname,
			"tsdproxy.port.http": "8080/http:8080/http",
		},
		WaitPort: "8080/tcp",
	})

	// Verify tsdproxy is still running (health endpoint returns 200).
	resp, err := http.Get(proxy.BaseURL + "/health/ready/")
	require.NoError(t, err, "failed to GET /health/ready/")
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode, "tsdproxy should still be healthy after duplicate hostname conflict")

	// Verify at least one of the proxies is reachable.
	client := NewTSNetClient(t, authKey)
	proxyURL := client.ProxyHTTPURL(hostname)

	assert.Eventually(t, func() bool {
		resp, err := client.Get(ctx, proxyURL)
		if err != nil {
			return false
		}
		resp.Body.Close()
		return true
	}, negativeWaitTimeout, negativePollInterval, "at least one proxy should be reachable despite duplicate hostname")

	t.Logf("tsdproxy log tail:\n%s", lastNLines(stripANSI(proxy.ReadLogFile(t)), 20))
}

func lastNLines(s string, n int) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) <= n {
		return s
	}
	return strings.Join(lines[len(lines)-n:], "\n")
}
