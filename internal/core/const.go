// SPDX-FileCopyrightText: 2024 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT
package core

import "time"

const (
	// G112 (CWE-400): Potential Slowloris Attack because ReadHeaderTimeout is not configured in the http.Server (Confidence: LOW, Severity: MEDIUM.
	ReadHeaderTimeout = 5 * time.Second
)
