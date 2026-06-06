// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	tailscale "tailscale.com/client/tailscale/v2"
)

const (
	envAuthKey      = "TSDPROXY_E2E_AUTHKEY"
	envAuthKeyFile  = "TSDPROXY_E2E_AUTHKEY_FILE"
	envClientID     = "TSDPROXY_E2E_CLIENTID"
	envClientSecret = "TSDPROXY_E2E_CLIENTSECRET"
	envTags         = "TS_TAGS"
	envCFApiToken   = "CF_API_TOKEN"
	envCFDomain     = "CF_DOMAIN"

	proxyStartupTimeout    = 120 * time.Second
	proxyReadyPollInterval = 1 * time.Second
	tsnetStartupTimeout    = 60 * time.Second

	testContainerImage = "nginx:alpine"

	// e2eBaseDir is the base directory for all e2e test data.
	e2eBaseDir = "/tmp/tsdproxy-e2e"

	e2eTestLabel = "tsdproxy.e2e=true"
)

var (
	tsAuthKey       string
	tsAuthKeyFile   string
	tsClientID      string
	tsClientSecret  string
	tsTags          string
	tsdproxyBinPath string
	projectRoot     string
	cfApiToken      string
	cfDomain        string
)

func TestMain(m *testing.M) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	tsAuthKey = os.Getenv(envAuthKey)
	tsAuthKeyFile = os.Getenv(envAuthKeyFile)
	tsClientID = os.Getenv(envClientID)
	tsClientSecret = os.Getenv(envClientSecret)
	tsTags = os.Getenv(envTags)
	if tsTags == "" {
		tsTags = "tag:tsdproxy-e2e"
	}
	cfApiToken = os.Getenv(envCFApiToken)
	cfDomain = os.Getenv(envCFDomain)

	os.RemoveAll(e2eBaseDir)
	os.MkdirAll(e2eBaseDir, 0o755)

	var err error
	projectRoot, err = resolveProjectRoot()
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
		os.Exit(1)
	}

	tsdproxyBinPath = filepath.Join(projectRoot, "tmp", "tsdproxy-e2e")
	if err := os.MkdirAll(filepath.Dir(tsdproxyBinPath), 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: failed to create output directory: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Building tsdproxy binary for e2e tests...")

	buildCmd := exec.CommandContext(ctx, "go", "build",
		"-o", tsdproxyBinPath,
		"./cmd/server/main.go",
	)
	buildCmd.Dir = projectRoot
	buildCmd.Stdout = os.Stdout
	buildCmd.Stderr = os.Stderr

	if err := buildCmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: failed to build tsdproxy: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("tsdproxy binary built successfully")

	cleanupTestContainers(ctx)
	cleanupTailscale(ctx)

	code := m.Run()

	postCtx, postCancel := context.WithTimeout(context.Background(), 2*time.Minute)
	cleanupTestContainers(postCtx)
	cleanupTailscale(postCtx)
	postCancel()

	os.Remove(tsdproxyBinPath)
	os.Exit(code)
}

func requireTailscaleAuth(t *testing.T) string {
	t.Helper()

	if tsAuthKey != "" {
		return tsAuthKey
	}

	if tsAuthKeyFile != "" {
		data, err := os.ReadFile(tsAuthKeyFile)
		if err != nil {
			t.Fatalf("failed to read auth key file %s: %v", tsAuthKeyFile, err)
		}
		return strings.TrimSpace(string(data))
	}

	t.Skip("TSDPROXY_E2E_AUTHKEY or TSDPROXY_E2E_AUTHKEY_FILE must be set for Tailscale tests")
	return ""
}

func requireOAuth(t *testing.T) {
	t.Helper()
	if tsClientID == "" || tsClientSecret == "" {
		t.Skip("TSDPROXY_E2E_CLIENTID and TSDPROXY_E2E_CLIENTSECRET must be set for OAuth tests")
	}
}

func requireCloudflare(t *testing.T) {
	t.Helper()
	if cfApiToken == "" || cfDomain == "" {
		t.Skip("CF_API_TOKEN and CF_DOMAIN must be set for Cloudflare DNS + ACME tests")
	}
}

func resolveProjectRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("get working directory: %w", err)
	}

	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("could not find go.mod in any parent directory")
		}
		dir = parent
	}
}

func cleanupTestContainers(ctx context.Context) {
	listCmd := exec.CommandContext(ctx, "docker", "ps", "-aq", "--filter", "label="+e2eTestLabel)
	out, err := listCmd.Output()
	if err != nil || len(out) == 0 {
		return
	}
	ids := strings.Fields(strings.TrimSpace(string(out)))
	if len(ids) == 0 {
		return
	}
	args := append([]string{"rm", "-f"}, ids...)
	cleanupCmd := exec.CommandContext(ctx, "docker", args...)
	cleanupCmd.Run()
}

func cleanupTailscale(ctx context.Context) {
	if tsClientID == "" || tsClientSecret == "" {
		fmt.Println("Cleanup: skipping Tailscale cleanup (no OAuth credentials)")
		return
	}

	client := &tailscale.Client{
		Tailnet: "-",
		Auth: &tailscale.OAuth{
			ClientID:     tsClientID,
			ClientSecret: tsClientSecret,
			Scopes:       []string{"devices:core", "services"},
		},
	}

	tag := tsTags
	cleanupTailscaleDevices(ctx, client, tag)
	cleanupTailscaleServices(ctx, client, tag)
}

func cleanupTailscaleDevices(ctx context.Context, client *tailscale.Client, tag string) {
	devices, err := client.Devices().List(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "WARNING: cleanupTailscaleDevices: failed to list devices: %v\n", err)
		return
	}

	deleted := 0
	for _, dev := range devices {
		if !slices.Contains(dev.Tags, tag) {
			continue
		}
		if err := client.Devices().Delete(ctx, dev.NodeID); err != nil {
			fmt.Fprintf(os.Stderr, "WARNING: cleanupTailscaleDevices: failed to delete %s (%s): %v\n", dev.NodeID, dev.Hostname, err)
			continue
		}
		deleted++
		fmt.Printf("Cleanup: deleted Tailscale device %s (%s)\n", dev.NodeID, dev.Hostname)
	}

	if deleted > 0 {
		fmt.Printf("Cleanup: removed %d Tailscale device(s) tagged %s\n", deleted, tag)
	} else {
		fmt.Printf("Cleanup: no Tailscale devices found tagged %s\n", tag)
	}
}

func cleanupTailscaleServices(ctx context.Context, client *tailscale.Client, tag string) {
	services, err := client.Services().List(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "WARNING: cleanupTailscaleServices: failed to list services: %v\n", err)
		return
	}

	deleted := 0
	for _, svc := range services {
		if !slices.Contains(svc.Tags, tag) {
			continue
		}
		if err := client.Services().Delete(ctx, svc.Name); err != nil {
			fmt.Fprintf(os.Stderr, "WARNING: cleanupTailscaleServices: failed to delete %s: %v\n", svc.Name, err)
			continue
		}
		deleted++
		fmt.Printf("Cleanup: deleted Tailscale service %s\n", svc.Name)
	}

	if deleted > 0 {
		fmt.Printf("Cleanup: removed %d Tailscale service(s) tagged %s\n", deleted, tag)
	} else {
		fmt.Printf("Cleanup: no Tailscale services found tagged %s\n", tag)
	}
}
