// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package consts

const (
	HeaderID            = "x-tsdproxy-id"
	HeaderUsername      = "x-tsdproxy-username"
	HeaderDisplayName   = "x-tsdproxy-displayname"
	HeaderProfilePicURL = "x-tsdproxy-profilepicurl"
	HeaderAuthToken     = "x-tsdproxy-auth-token" //nolint:gosec // G101: header name, not a credential

	HeaderRemoteUser                  = "Remote-User"
	HeaderXForwardedUser              = "X-Forwarded-User"
	HeaderXAuthRequestUser            = "X-Auth-Request-User"
	HeaderXForwardedEmail             = "X-Forwarded-Email"
	HeaderXAuthRequestEmail           = "X-Auth-Request-Email"
	HeaderXForwardedPreferredUsername = "X-Forwarded-Preferred-Username"

	// HeaderRealIP is the de-facto standard header for passing the single
	// authoritative client IP address to upstream services (popularized by
	// nginx).  Unlike X-Forwarded-For (which may carry a comma-separated
	// chain of proxies), X-Real-IP always contains exactly one IP.
	HeaderRealIP = "X-Real-IP"
)
