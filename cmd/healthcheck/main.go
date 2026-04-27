// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package main

import (
	"net/http"
	"os"
	"time"
)

func main() {
	client := &http.Client{
		Timeout: 5 * time.Second, //nolint:mnd
	}
	resp, err := client.Get("http://127.0.0.1:8080/health/ready/")
	if err != nil {
		os.Exit(1)
	}
	resp.Body.Close()
}
