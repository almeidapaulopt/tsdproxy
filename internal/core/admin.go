// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package core

import (
	"net"
	"net/http"
	"slices"

	"github.com/almeidapaulopt/tsdproxy/internal/config"
	"github.com/almeidapaulopt/tsdproxy/internal/consts"
	"github.com/almeidapaulopt/tsdproxy/internal/model"
)

// AdminMiddleware enforces the admin allowlist. When config.Config.Admins
// is empty the middleware is a no-op.
//
// Identity is resolved in priority order:
//  1. whoisFunc (direct tsnet connections — authoritative)
//  2. x-tsdproxy-id header, only when the request originates from localhost
//     (set by the in-process reverse proxy after stripping client headers)
//
// When no identity can be resolved, config.Config.AdminAllowLocalhost
// controls whether localhost requests are permitted without auth.
func AdminMiddleware(whoisFunc func(r *http.Request) model.Whois) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			admins := config.Config.Admins
			if len(admins) == 0 {
				next.ServeHTTP(w, r)
				return
			}

			id := resolveAdminID(r, whoisFunc)
			if id != "" {
				if slices.Contains(admins, id) {
					next.ServeHTTP(w, r)
					return
				}
				http.Error(w, "access denied", http.StatusForbidden)
				return
			}

			if isLocalhost(r.RemoteAddr) && config.Config.AdminAllowLocalhost {
				next.ServeHTTP(w, r)
				return
			}

			http.Error(w, "admin access requires a Tailscale connection", http.StatusForbidden)
		})
	}
}

// resolveWhois resolves the Tailscale identity for a request using
// whoisFunc first (context — authoritative, from direct tsnet connections)
// then falling back to x-tsdproxy-* headers when config.Config.AdminAllowLocalhost
// is true and the request originates from localhost (set by the in-process
// reverse proxy after stripping client-supplied headers).
func resolveWhois(r *http.Request, whoisFunc func(r *http.Request) model.Whois) model.Whois {
	if who := whoisFunc(r); who.ID != "" {
		return who
	}

	if !config.Config.AdminAllowLocalhost || !isLocalhost(r.RemoteAddr) {
		return model.Whois{}
	}

	return model.Whois{
		ID:            r.Header.Get(consts.HeaderID),
		Username:      r.Header.Get(consts.HeaderUsername),
		DisplayName:   r.Header.Get(consts.HeaderDisplayName),
		ProfilePicURL: r.Header.Get(consts.HeaderProfilePicURL),
	}
}

func resolveAdminID(r *http.Request, whoisFunc func(r *http.Request) model.Whois) string {
	return resolveWhois(r, whoisFunc).ID
}

func isLocalhost(remoteAddr string) bool {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}

	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}

	return ip.IsLoopback()
}

// StripProxyIdentityHeaders removes x-tsdproxy-* headers from incoming
// requests that do not originate from localhost. Locally-forwarded requests
// from the in-process reverse proxy are permitted to carry these headers;
// external clients and user containers must not.
func StripProxyIdentityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isLocalhost(r.RemoteAddr) {
			r.Header.Del(consts.HeaderID)
			r.Header.Del(consts.HeaderUsername)
			r.Header.Del(consts.HeaderDisplayName)
			r.Header.Del(consts.HeaderProfilePicURL)
		}
		next.ServeHTTP(w, r)
	})
}

func WhoAmIHandler(srv *HTTPServer, whoisFunc func(r *http.Request) model.Whois) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		who := whoisFunc(r)
		if who.ID == "" && isLocalhost(r.RemoteAddr) {
			who = model.Whois{
				ID:            r.Header.Get(consts.HeaderID),
				Username:      r.Header.Get(consts.HeaderUsername),
				DisplayName:   r.Header.Get(consts.HeaderDisplayName),
				ProfilePicURL: r.Header.Get(consts.HeaderProfilePicURL),
			}
		}
		if who.ID == "" {
			srv.ErrorResponse(w, r, nil, "no Tailscale identity found", http.StatusUnauthorized)
			return
		}

		type whoamiResponse struct {
			ID            string `json:"id"`
			DisplayName   string `json:"displayName"`
			Username      string `json:"username"`
			ProfilePicURL string `json:"profilePicUrl"`
		}

		srv.JSONResponse(w, r, whoamiResponse{
			ID:            who.ID,
			DisplayName:   who.DisplayName,
			Username:      who.Username,
			ProfilePicURL: who.ProfilePicURL,
		})
	}
}
