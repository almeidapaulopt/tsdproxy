// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package proxymanager

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/time/rate"
)

// TestRateLimitMiddleware_AppliesWhenPeerIPUnresolvable_BUG reproduces H-2:
// when resolvePeerIP returns "" (e.g. localhost RemoteAddr + missing XFF, or
// spoofed multi-valued XFF), rateLimitMiddleware silently exempts the request.
//
// In shared SNI / services VIP mode every inbound request has RemoteAddr=loopback
// (PortRouter hop), so an attacker who sends zero or multiple XFF headers
// bypasses rate limiting entirely.
//
// Expected (post-fix) behavior: the middleware should fall back to a bucket key
// derived from r.RemoteAddr (or any stable identifier) and still apply the limit.
//
// Today: assertion fails because the middleware returns 200 instead of 429.
func TestRateLimitMiddleware_AppliesWhenPeerIPUnresolvable_BUG(t *testing.T) {
	t.Parallel()

	// 0/0 limiter rejects every request.
	limiter := newIPRateLimiter(rate.Limit(0), 0)

	handler := rateLimitMiddleware(limiter, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Request that triggers resolvePeerIP to return "":
	//   - RemoteAddr loopback (services/VIP mode PortRouter hop)
	//   - No X-Forwarded-For header (len(xffVals) == 0, fails != 1 check)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "127.0.0.1:4321"
	// Intentionally no X-Forwarded-For header.

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	// A 0-rate / 0-burst limiter rejects ALL requests. The middleware must
	// therefore return 429 regardless of whether the peer IP was resolvable.
	//
	// Bug: today returns 200 because `ip == ""` short-circuits the limiter
	// check at ratelimit.go:83.
	require.Equal(t, http.StatusTooManyRequests, rec.Code,
		"BUG: request with unresolvable peer IP (localhost + no XFF) bypassed "+
			"rate limiting — middleware should fall back to RemoteAddr bucket")
}

// TestRateLimitMiddleware_AppliesWhenXFFHasMultipleValues_BUG reproduces the
// same H-2 bug via a different trigger: multiple XFF headers cause
// resolvePeerIP to return "" (ratelimit.go:108 — len(xffVals) != 1).
//
// An attacker can append their own X-Forwarded-For header on top of the
// PortRouter's legitimate value to trip the strict-uniqueness check and
// bypass rate limiting.
func TestRateLimitMiddleware_AppliesWhenXFFHasMultipleValues_BUG(t *testing.T) {
	t.Parallel()

	limiter := newIPRateLimiter(rate.Limit(0), 0)

	handler := rateLimitMiddleware(limiter, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "127.0.0.1:4321"
	// Legitimate hop value set by PortRouter
	req.Header.Add("X-Forwarded-For", "100.64.0.5")
	// Attacker-appended duplicate to trigger len(xffVals) != 1
	req.Header.Add("X-Forwarded-For", "1.2.3.4")

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusTooManyRequests, rec.Code,
		"BUG: request with multi-valued X-Forwarded-For bypassed rate limiting — "+
			"strict-uniqueness check should fall back to a default bucket, not exempt the request")
}

// TestRateLimitMiddleware_AppliesWhenXFFIsLoopback_BUG reproduces H-2 via the
// third trigger: resolvePeerIP returns "" when XFF is a loopback address
// (ratelimit.go:115 — IsLoopback check rejects, returns "").
//
// In services/VIP mode an attacker can spoof X-Forwarded-For: 127.0.0.1 to
// bypass rate limiting.
func TestRateLimitMiddleware_AppliesWhenXFFIsLoopback_BUG(t *testing.T) {
	t.Parallel()

	limiter := newIPRateLimiter(rate.Limit(0), 0)

	handler := rateLimitMiddleware(limiter, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "127.0.0.1:4321"
	req.Header.Set("X-Forwarded-For", "127.0.0.1")

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusTooManyRequests, rec.Code,
		"BUG: request with loopback XFF value bypassed rate limiting — "+
			"loopback rejection in resolvePeerIP should not exempt the request entirely")
}
