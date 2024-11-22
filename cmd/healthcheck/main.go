// SPDX-FileCopyrightText: 2024 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT
package main

import (
	"net/http"
	"os"
)

func main() {
	h, err := http.Get("http://127.0.0.1:8080/health/ready/")
	if err != nil {
		os.Exit(1)
	}
	h.Body.Close()
}
