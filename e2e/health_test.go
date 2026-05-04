// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHealthEndpointReady(t *testing.T) {
	proxy := StartTSDProxy(t, TSDProxyConfig{
		AuthKey: requireTailscaleAuth(t),
	})

	resp, err := http.Get(proxy.BaseURL + "/health/ready/")
	require.NoError(t, err, "failed to GET /health/ready/")
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err, "failed to read response body")

	assert.Equal(t, http.StatusOK, resp.StatusCode, "unexpected status code, body: %s", string(body))
	assert.Contains(t, string(body), "OK", "response body should contain OK")
}

func TestHealthEndpointDuringShutdown(t *testing.T) {
	proxy := StartTSDProxy(t, TSDProxyConfig{
		AuthKey: requireTailscaleAuth(t),
	})

	resp, err := http.Get(proxy.BaseURL + "/health/ready/")
	require.NoError(t, err, "failed to GET /health/ready/ before shutdown")
	resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode, "expected 200 before shutdown")

	// Cleanup in StartTSDProxy also calls Stop, but we test shutdown explicitly.
	proxy.Stop(t)

	assert.Eventually(t, func() bool {
		resp, err := http.Get(proxy.BaseURL + "/health/ready/")
		if err != nil {
			return true
		}
		resp.Body.Close()
		return false
	}, 5*time.Second, 100*time.Millisecond, "expected connection error after shutdown")
}

func TestHealthEndpointBeforeStartup(t *testing.T) {
	cfg := defaultTSDProxyConfig()
	cfg.AuthKey = requireTailscaleAuth(t)

	tmpDir := e2eTestDataDir(t)
	dataDir := filepath.Join(tmpDir, "data")
	require.NoError(t, os.MkdirAll(dataDir, 0o755))

	configPath := filepath.Join(tmpDir, "tsdproxy.yaml")
	configContent := generateConfig(configParams{
		HTTPPort:       cfg.HTTPPort,
		AuthKey:        cfg.AuthKey,
		DataDir:        dataDir,
		TargetHostname: cfg.TargetHostname,
	})
	require.NoError(t, os.WriteFile(configPath, []byte(configContent), 0o644))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cmd := exec.CommandContext(ctx, tsdproxyBinPath, "-config", configPath)
	cmd.Env = append(os.Environ(),
		fmt.Sprintf("TSDPROXY_HTTP_PORT=%d", cfg.HTTPPort),
	)
	cmd.Stdout = &testLogWriter{t: t, prefix: "[tsdproxy-raw] "}
	cmd.Stderr = &testLogWriter{t: t, prefix: "[tsdproxy-raw] "}

	require.NoError(t, cmd.Start(), "failed to start tsdproxy manually")

	t.Cleanup(func() {
		if cmd.Process != nil {
			cmd.Process.Signal(os.Interrupt)
			done := make(chan error, 1)
			go func() { done <- cmd.Wait() }()
			select {
			case <-done:
			case <-time.After(15 * time.Second):
				cmd.Process.Kill()
			}
		}
	})

	baseURL := fmt.Sprintf("http://127.0.0.1:%d", cfg.HTTPPort)
	healthURL := baseURL + "/health/ready/"

	var sawReady bool
	var sawNotReadyHTTP bool

	deadline := time.Now().Add(proxyStartupTimeout)
	for time.Now().Before(deadline) {
		resp, err := http.Get(healthURL)
		if err != nil {
			time.Sleep(10 * time.Millisecond)
			continue
		}
		io.ReadAll(resp.Body)
		resp.Body.Close()

		if resp.StatusCode == http.StatusOK {
			sawReady = true
			break
		}

		sawNotReadyHTTP = true
		assert.Equal(t, http.StatusServiceUnavailable, resp.StatusCode,
			"pre-ready health response must be 503, got %d", resp.StatusCode)
		time.Sleep(10 * time.Millisecond)
	}

	require.True(t, sawReady, "health endpoint never returned 200 OK within %v", proxyStartupTimeout)
	if !sawNotReadyHTTP {
		t.Log("startup transitioned directly from not-listening to ready; no pre-ready HTTP response was observed")
	}
}
