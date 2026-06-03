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
)

// IdentityHeaders lists the x-tsdproxy-* headers that carry authenticated
// user identity.  Both the reverse proxy (injection) and the management
// server (stripping) use this list to stay in sync.
var IdentityHeaders = []string{
	HeaderID,
	HeaderUsername,
	HeaderDisplayName,
	HeaderProfilePicURL,
}

// TrustedProxyHeaders lists all headers that the reverse proxy may set on
// outbound requests (identity headers + standard auth-proxy headers + the
// internal auth token).  The reverse proxy strips all of these from
// client-supplied requests before re-injecting authenticated values.
var TrustedProxyHeaders = []string{
	HeaderID,
	HeaderUsername,
	HeaderDisplayName,
	HeaderProfilePicURL,
	HeaderAuthToken,
	HeaderRemoteUser,
	HeaderXForwardedUser,
	HeaderXAuthRequestUser,
	HeaderXForwardedEmail,
	HeaderXAuthRequestEmail,
	HeaderXForwardedPreferredUsername,
}
