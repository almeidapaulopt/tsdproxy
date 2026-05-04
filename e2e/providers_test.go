// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDockerProviderDefault(t *testing.T) {
	authKey := requireTailscaleAuth(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	proxy := StartTSDProxy(t, TSDProxyConfig{
		AuthKey: authKey,
	})
	t.Logf("tsdproxy started on port %d", proxy.HTTPPort)

	hostname := fmt.Sprintf("e2e-docker-default-%d", time.Now().UnixNano())
	StartContainer(t, ContainerConfig{
		Labels: map[string]string{
			"tsdproxy.enable":      "true",
			"tsdproxy.name":        hostname,
			"tsdproxy.port.http":   "80/http:80/http",
		},
	})

	client := NewTSNetClient(t, authKey)
	proxyURL := client.ProxyHTTPURL(hostname)
	WaitForProxyReachable(t, ctx, client, proxyURL, 60*time.Second)
	VerifyHTTPResponse(t, ctx, client, proxyURL, "Welcome to nginx!")
}

func TestListProviderBasic(t *testing.T) {
	authKey := requireTailscaleAuth(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	// Start nginx container WITHOUT tsdproxy labels — just expose port 80.
	ctr := StartContainer(t, ContainerConfig{
		ExposedPorts: []string{"80/tcp"},
	})

	mappedPort, err := ctr.MappedPort(ctx, "80/tcp")
	require.NoError(t, err, "failed to get mapped port for nginx container")
	targetURL := fmt.Sprintf("http://127.0.0.1:%s", mappedPort.Port())
	t.Logf("list provider target: %s", targetURL)

	// Generate list provider file pointing to the nginx container.
	hostname := fmt.Sprintf("e2e-list-provider-%d", time.Now().UnixNano())
	listFilePath := GenerateListProviderFile(t, map[string]ListEntry{
		hostname: {
			ProxyProvider: "default",
			Dashboard:     ListDashboard{Visible: true, Label: "List Provider Test"},
			Ports: map[string]ListPort{
				"80/http": {
					Targets: []string{targetURL},
				},
			},
		},
	})

	// Build tsdproxy config with lists section.
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
	t.Logf("tsdproxy with list provider started on port %d", proxy.HTTPPort)

	client := NewTSNetClient(t, authKey)
	proxyURL := client.ProxyHTTPURL(hostname)
	WaitForProxyReachable(t, ctx, client, proxyURL, 60*time.Second)
	VerifyHTTPResponse(t, ctx, client, proxyURL, "Welcome to nginx!")
}

func TestProxyProviderLabelOverride(t *testing.T) {
	authKey := requireTailscaleAuth(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	// Keep a working default provider, then point one container at a missing
	// provider. If the label is ignored, that container would still come up via
	// the default provider, so this verifies the override is actually consulted.
	httpPort := getFreePort()
	tmpDir := e2eTestDataDir(t)
	dataDir := filepath.Join(tmpDir, "data")
	require.NoError(t, os.MkdirAll(dataDir, 0o755))

	configContent := fmt.Sprintf(`defaultProxyProvider: default
docker:
  local:
    targetHostname: "172.17.0.1"
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
`, authKey, dataDir, httpPort)

	proxy := startTSDProxyRawConfig(t, configContent, httpPort, tmpDir, dataDir)
	t.Logf("tsdproxy with custom provider started on port %d", proxy.HTTPPort)

	client := NewTSNetClient(t, authKey)

	// Container 1: explicitly selects a missing provider and must fail.
	hostname1 := fmt.Sprintf("e2e-provider-missing-%d", time.Now().UnixNano())
	StartContainer(t, ContainerConfig{
		Labels: map[string]string{
			"tsdproxy.enable":        "true",
			"tsdproxy.name":          hostname1,
			"tsdproxy.proxyprovider": "missingprovider",
			"tsdproxy.port.http":     "80/http:80/http",
		},
	})

	proxyURL1 := client.ProxyHTTPURL(hostname1)
	assert.Never(t, func() bool {
		resp, err := client.Get(ctx, proxyURL1)
		if err != nil {
			return false
		}
		resp.Body.Close()
		return true
	}, 20*time.Second, 2*time.Second, "proxy with missing provider override should not become reachable")

	// Container 2: no proxyprovider label — falls through to the working default provider.
	hostname2 := fmt.Sprintf("e2e-provider-default-%d", time.Now().UnixNano())
	StartContainer(t, ContainerConfig{
		Labels: map[string]string{
			"tsdproxy.enable":    "true",
			"tsdproxy.name":      hostname2,
			"tsdproxy.port.http": "80/http:80/http",
		},
	})

	proxyURL2 := client.ProxyHTTPURL(hostname2)
	WaitForProxyReachable(t, ctx, client, proxyURL2, 60*time.Second)
	VerifyHTTPResponse(t, ctx, client, proxyURL2, "Welcome to nginx!")

	// Verify the missing-provider override was actually attempted.
	logContent := proxy.ReadLogFile(t)
	require.NotEmpty(t, logContent, "tsdproxy log should not be empty")
	assert.Contains(t, strings.ToLower(logContent), "proxyprovider not found",
		"tsdproxy log should show the missing provider override was rejected")
}

func TestOAuthAuth(t *testing.T) {
	requireOAuth(t)
	authKey := requireTailscaleAuth(t) // auth key still needed for the tsnet test client

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	httpPort := getFreePort()
	tmpDir := e2eTestDataDir(t)
	dataDir := filepath.Join(tmpDir, "data")
	require.NoError(t, os.MkdirAll(dataDir, 0o755))

	configContent := generateConfig(configParams{
		HTTPPort:     httpPort,
		DataDir:      dataDir,
		ClientID:     tsClientID,
		ClientSecret: tsClientSecret,
		Tags:         tsTags,
	})

	proxy := startTSDProxyRawConfig(t, configContent, httpPort, tmpDir, dataDir)
	t.Logf("tsdproxy with OAuth started on port %d", proxy.HTTPPort)

	hostname := fmt.Sprintf("e2e-oauth-test-%d", time.Now().UnixNano())
	StartContainer(t, ContainerConfig{
		Labels: map[string]string{
			"tsdproxy.enable":      "true",
			"tsdproxy.name":        hostname,
			"tsdproxy.port.http":   "80/http:80/http",
		},
	})

	client := NewTSNetClient(t, authKey)
	proxyURL := client.ProxyHTTPURL(hostname)
	WaitForProxyReachable(t, ctx, client, proxyURL, 60*time.Second)
	VerifyHTTPResponse(t, ctx, client, proxyURL, "Welcome to nginx!")
}

func TestAuthKeyFile(t *testing.T) {
	authKey := requireTailscaleAuth(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	authKeyFile := filepath.Join(t.TempDir(), "tsdproxy-authkey")
	require.NoError(t, os.WriteFile(authKeyFile, []byte(authKey), 0o600), "failed to write auth key file")

	proxy := StartTSDProxy(t, TSDProxyConfig{
		AuthKeyFile: authKeyFile,
	})
	t.Logf("tsdproxy started with authkeyfile on port %d", proxy.HTTPPort)

	hostname := fmt.Sprintf("e2e-authkeyfile-%d", time.Now().UnixNano())
	StartContainer(t, ContainerConfig{
		Labels: map[string]string{
			"tsdproxy.enable":      "true",
			"tsdproxy.name":        hostname,
			"tsdproxy.port.http":   "80/http:80/http",
		},
	})

	client := NewTSNetClient(t, authKey)
	proxyURL := client.ProxyHTTPURL(hostname)
	WaitForProxyReachable(t, ctx, client, proxyURL, 60*time.Second)
	VerifyHTTPResponse(t, ctx, client, proxyURL, "Welcome to nginx!")
}

// startTSDProxyRawConfig starts a tsdproxy instance from a raw YAML config string.
func startTSDProxyRawConfig(t *testing.T, configContent string, httpPort int, tmpDir, dataDir string) *TSDProxyInstance {
	t.Helper()
	ctx := context.Background()

	configPath := filepath.Join(tmpDir, "tsdproxy.yaml")
	require.NoError(t, os.WriteFile(configPath, []byte(configContent), 0o644))

	cmd := exec.CommandContext(ctx, tsdproxyBinPath, "-config", configPath)
	cmd.Env = append(os.Environ(),
		fmt.Sprintf("TSDPROXY_HTTP_PORT=%d", httpPort),
	)

	logFile, err := os.Create(filepath.Join(tmpDir, "tsdproxy.log"))
	require.NoError(t, err)

	cmd.Stdout = io.MultiWriter(logFile, &testLogWriter{t: t, prefix: "[tsdproxy] "})
	cmd.Stderr = io.MultiWriter(logFile, &testLogWriter{t: t, prefix: "[tsdproxy] "})

	require.NoError(t, cmd.Start(), "failed to start tsdproxy")

	instance := &TSDProxyInstance{
		cmd:     cmd,
		BaseURL: fmt.Sprintf("http://127.0.0.1:%d", httpPort),
		HTTPPort: httpPort,
		TmpDir:   tmpDir,
		DataDir:  dataDir,
		Config:   configPath,
	}
	go instance.exitOnce.Do(func() {
		instance.exitErr = cmd.Wait()
		instance.exited.Store(true)
	})

	t.Cleanup(func() {
		instance.Stop(t)
		logFile.Close()
	})

	instance.WaitReady(t)

	return instance
}
