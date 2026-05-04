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
	"strings"
	"testing"
	"time"
)

const (
	envAuthKey      = "TS_AUTHKEY"
	envAuthKeyFile  = "TS_AUTHKEY_FILE"
	envClientID     = "TS_CLIENT_ID"
	envClientSecret = "TS_CLIENT_SECRET"
	envTags         = "TS_TAGS"

	proxyStartupTimeout    = 120 * time.Second
	proxyReadyPollInterval = 1 * time.Second
	tsnetStartupTimeout    = 60 * time.Second

	testContainerImage = "nginx:alpine"

	// e2eBaseDir is the base directory for all e2e test data.
	e2eBaseDir = "/tmp/tsdproxy-e2e"
)

var (
	tsAuthKey       string
	tsAuthKeyFile   string
	tsClientID      string
	tsClientSecret  string
	tsTags          string
	tsdproxyBinPath string
	projectRoot     string
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

	code := m.Run()
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

	t.Skip("TS_AUTHKEY or TS_AUTHKEY_FILE must be set for Tailscale tests")
	return ""
}

func requireOAuth(t *testing.T) {
	t.Helper()
	if tsClientID == "" || tsClientSecret == "" {
		t.Skip("TS_CLIENT_ID and TS_CLIENT_SECRET must be set for OAuth tests")
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
