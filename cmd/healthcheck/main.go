// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package main

import (
	"net/http"
	"os"
	"strings"
	"time"
)

func main() {
	port := readPort()
	if port == "" {
		port = "8080"
	}

	client := &http.Client{
		Timeout: 5 * time.Second, //nolint:mnd
	}
	resp, err := client.Get("http://127.0.0.1:" + port + "/health/ready/") //nolint:gosec // G704: port is from file/env, not user input
	if err != nil {
		os.Exit(1)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		os.Exit(1)
	}
}

func readPort() string {
	dataDir := os.Getenv("TSDPROXY_DATADIR")
	if dataDir == "" {
		dataDir = "/data"
	}
	if data, err := os.ReadFile(dataDir + "/.http-port"); err == nil {
		if p := strings.TrimSpace(string(data)); p != "" {
			return p
		}
	}
	return os.Getenv("TSDPROXY_HTTP_PORT")
}
