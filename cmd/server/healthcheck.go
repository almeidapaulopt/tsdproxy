// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package main

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const healthCheckTimeout = 5 * time.Second

func isHealthcheckSubcommand() bool {
	return len(os.Args) > 1 && os.Args[1] == "healthcheck"
}

func runHealthcheck() int {
	port := readHealthcheckPort()
	if port == "" {
		port = "8080"
	}

	client := &http.Client{
		Timeout: healthCheckTimeout,
	}
	resp, err := client.Get("http://127.0.0.1:" + port + "/health/ready/") //nolint:gosec // G704: port is from file/env, not user input
	if err != nil {
		return 1
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 1
	}
	return 0
}

func readHealthcheckPort() string {
	dataDir := os.Getenv("TSDPROXY_DATADIR")
	if dataDir == "" {
		dataDir = "/data"
	}
	if data, err := os.ReadFile(filepath.Join(dataDir, ".http-port")); err == nil { //nolint:gosec // G703: dataDir from env, not user input
		if p := strings.TrimSpace(string(data)); p != "" {
			return p
		}
	}
	return os.Getenv("TSDPROXY_HTTP_PORT")
}
