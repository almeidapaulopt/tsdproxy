// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package core

import "net/http"

// csrfProtection uses the Go 1.25+ standard library cross-origin protection
// which rejects non-safe cross-origin browser requests via Sec-Fetch-Site
// and Origin header validation.
var csrfProtection = http.NewCrossOriginProtection()

// CSRFMiddleware wraps handlers with cross-origin request protection.
func CSRFMiddleware(next http.Handler) http.Handler {
	return csrfProtection.Handler(next)
}
