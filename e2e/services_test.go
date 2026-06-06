// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Services Mode Helpers ---

// servicesModeLabels returns Docker labels for a container proxied via
// Tailscale VIP Services mode.
func servicesModeLabels(name string) map[string]string {
	return map[string]string{
		"tsdproxy.enable":        "true",
		"tsdproxy.name":          name,
		"tsdproxy.ephemeral":     "true",
		"tsdproxy.proxyprovider": "services",
		"tsdproxy.port.https":    "443/https:80/http",
	}
}

// startServicesProxy builds and starts a tsdproxy instance configured with
// a services (VIP) provider. Requires OAuth credentials.
func startServicesProxy(t *testing.T) *TSDProxyInstance {
	t.Helper()
	requireOAuth(t)

	httpPort := getFreePort()
	tmpDir := e2eTestDataDir(t)
	dataDir := filepath.Join(tmpDir, "data")
	require.NoError(t, os.MkdirAll(dataDir, 0o755))

	svcHostname := fmt.Sprintf("e2e-svc-server-%d", time.Now().UnixNano())

	configContent := fmt.Sprintf(`defaultProxyProvider: services
docker:
  local:
    targetHostname: "172.17.0.1"
tailscale:
  providers:
    services:
      services: true
      hostname: %q
      clientId: %q
      clientSecret: %q
      tags: %q
      autoApproveDevices: true
  dataDir: %q
http:
  hostname: "0.0.0.0"
  port: %d
log:
  level: debug
  json: false
proxyAccessLog: true
`, svcHostname, tsClientID, tsClientSecret, tsTags, dataDir, httpPort)

	return startTSDProxyRawConfig(t, configContent, httpPort, tmpDir, dataDir)
}

// discoverServicesFQDN polls the tsdproxy log for the "service proxy started"
// line and returns the FQDN that Tailscale auto-assigned to the VIP service.
// The hostname parameter is the tsdproxy.name label value (without "svc:" prefix).
func discoverServicesFQDN(t *testing.T, ctx context.Context, proxy *TSDProxyInstance, hostname string) string {
	t.Helper()

	// Match zerolog console format: ... fqdn=<value> ... msg="service proxy started"
	re := regexp.MustCompile(`fqdn=(\S+).*msg="service proxy started"`)

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		log := proxy.ReadLogFile(t)
		if m := re.FindStringSubmatch(log); m != nil {
			t.Logf("discovered services FQDN for %q: %s", hostname, m[1])
			return m[1]
		}
		select {
		case <-ctx.Done():
			logLines := strings.Split(log, "\n")
			start := len(logLines) - 30
			if start < 0 {
				start = 0
			}
			tail := strings.Join(logLines[start:], "\n")
			t.Fatalf("timed out waiting for VIP FQDN for %q in logs; tail:\n%s", hostname, tail)
			return ""
		case <-ticker.C:
		}
	}
}

// waitForServicesProxy polls until a services-mode VIP proxy is reachable
// via the auto-assigned FQDN.
func waitForServicesProxy(t *testing.T, ctx context.Context, client *TSNetClient, fqdn string, timeout time.Duration) {
	t.Helper()
	proxyURL := fmt.Sprintf("https://%s/", fqdn)
	WaitForProxyReachable(t, ctx, client, proxyURL, timeout)
}

func verifyServicesProxy(t *testing.T, ctx context.Context, client *TSNetClient, fqdn, expectedSubstring string) {
	t.Helper()
	proxyURL := fmt.Sprintf("https://%s/", fqdn)
	VerifyHTTPResponse(t, ctx, client, proxyURL, expectedSubstring)
}

// --- Tests ---

// TestServicesModeBasic verifies that a container proxied via Tailscale VIP
// Services mode gets an auto-assigned FQDN and is reachable through it.
func TestServicesModeBasic(t *testing.T) {
	requireOAuth(t)
	authKey := requireTailscaleAuth(t)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	proxy := startServicesProxy(t)
	client := NewTSNetClient(t, authKey)

	hostname := fmt.Sprintf("e2e-svc-basic-%d", time.Now().UnixNano())
	StartContainer(t, ContainerConfig{Labels: servicesModeLabels(hostname)})

	fqdn := discoverServicesFQDN(t, ctx, proxy, hostname)
	require.NotEmpty(t, fqdn)

	waitForServicesProxy(t, ctx, client, fqdn, 90*time.Second)
	verifyServicesProxy(t, ctx, client, fqdn, "Welcome to nginx!")
}

// TestServicesModeMultipleContainers verifies that two containers proxied
// via the same services server get distinct VIP FQDNs and are both reachable.
func TestServicesModeMultipleContainers(t *testing.T) {
	requireOAuth(t)
	authKey := requireTailscaleAuth(t)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	proxy := startServicesProxy(t)
	client := NewTSNetClient(t, authKey)

	ts := time.Now().UnixNano()
	hostname1 := fmt.Sprintf("e2e-svc-multi-1-%d", ts)
	hostname2 := fmt.Sprintf("e2e-svc-multi-2-%d", ts)

	StartContainer(t, ContainerConfig{Labels: servicesModeLabels(hostname1)})
	StartContainer(t, ContainerConfig{Labels: servicesModeLabels(hostname2)})

	fqdn1 := discoverServicesFQDN(t, ctx, proxy, hostname1)
	require.NotEmpty(t, fqdn1)
	fqdn2 := discoverServicesFQDN(t, ctx, proxy, hostname2)
	require.NotEmpty(t, fqdn2)

	// Verify distinct FQDNs
	require.NotEqual(t, fqdn1, fqdn2, "two containers should get distinct VIP FQDNs")

	waitForServicesProxy(t, ctx, client, fqdn1, 120*time.Second)
	waitForServicesProxy(t, ctx, client, fqdn2, 120*time.Second)

	verifyServicesProxy(t, ctx, client, fqdn1, "Welcome to nginx!")
	verifyServicesProxy(t, ctx, client, fqdn2, "Welcome to nginx!")
}

// TestServicesModeStopRemovesService verifies that stopping a container
// removes the VIP service and makes the proxy unreachable.
func TestServicesModeStopRemovesService(t *testing.T) {
	requireOAuth(t)
	authKey := requireTailscaleAuth(t)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	proxy := startServicesProxy(t)
	client := NewTSNetClient(t, authKey)

	hostname := fmt.Sprintf("e2e-svc-stop-%d", time.Now().UnixNano())
	ctr := StartContainer(t, ContainerConfig{Labels: servicesModeLabels(hostname)})

	fqdn := discoverServicesFQDN(t, ctx, proxy, hostname)
	require.NotEmpty(t, fqdn)

	proxyURL := fmt.Sprintf("https://%s/", fqdn)
	waitForServicesProxy(t, ctx, client, fqdn, 90*time.Second)
	verifyServicesProxy(t, ctx, client, fqdn, "Welcome to nginx!")

	// Stop the container and verify the proxy becomes unreachable.
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
	}, 60*time.Second, 2*time.Second, "expected services proxy %s to become unreachable after container stop", fqdn)
}

// TestServicesModeRestartWithNewService verifies that after stopping a
// container and starting a new one, the new container gets a new VIP FQDN
// and is reachable.
func TestServicesModeRestartWithNewService(t *testing.T) {
	requireOAuth(t)
	authKey := requireTailscaleAuth(t)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	proxy := startServicesProxy(t)
	client := NewTSNetClient(t, authKey)

	// Phase 1: Start first container and verify reachable.
	ts := time.Now().UnixNano()
	hostname1 := fmt.Sprintf("e2e-svc-restart-1-%d", ts)
	ctr1 := StartContainer(t, ContainerConfig{Labels: servicesModeLabels(hostname1)})

	fqdn1 := discoverServicesFQDN(t, ctx, proxy, hostname1)
	require.NotEmpty(t, fqdn1)

	waitForServicesProxy(t, ctx, client, fqdn1, 90*time.Second)
	verifyServicesProxy(t, ctx, client, fqdn1, "Welcome to nginx!")

	// Phase 2: Stop first container.
	stopCtx, stopCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer stopCancel()
	require.NoError(t, ctr1.Stop(stopCtx, nil), "failed to stop container 1")

	// Phase 3: Start a new container and verify it gets a new FQDN.
	hostname2 := fmt.Sprintf("e2e-svc-restart-2-%d", ts)
	StartContainer(t, ContainerConfig{Labels: servicesModeLabels(hostname2)})

	fqdn2 := discoverServicesFQDN(t, ctx, proxy, hostname2)
	require.NotEmpty(t, fqdn2)

	// The new FQDN should be different from the old one.
	require.NotEqual(t, fqdn1, fqdn2, "new container should get a distinct VIP FQDN")

	waitForServicesProxy(t, ctx, client, fqdn2, 120*time.Second)
	verifyServicesProxy(t, ctx, client, fqdn2, "Welcome to nginx!")
}
