// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package core

import (
	"encoding/json"
	"net"
	"net/http"
	"slices"

	"github.com/rs/zerolog/log"

	"github.com/almeidapaulopt/tsdproxy/internal/config"
	"github.com/almeidapaulopt/tsdproxy/internal/consts"
	"github.com/almeidapaulopt/tsdproxy/internal/model"
)

// AdminMiddleware enforces the admin allowlist. When config.Config.Admins
// is empty the middleware is a no-op. Identity is resolved in priority order:
//  1. Context (set by ProviderUserMiddleware on direct tsnet connections)
//  2. x-tsdproxy-* headers, only from localhost (set by the in-process
//     reverse proxy after stripping client headers)
//
// When no identity can be resolved, config.Config.AdminAllowLocalhost
// controls whether localhost requests are permitted without auth.
func AdminMiddleware() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			admins := config.Config.Admins
			if len(admins) == 0 {
				next.ServeHTTP(w, r)
				return
			}

			id := ResolveWhois(r).ID
			if id != "" {
				if slices.Contains(admins, id) {
					next.ServeHTTP(w, r)
					return
				}
				writeForbidden(w, "access denied")
				return
			}

			if IsLocalhost(r.RemoteAddr) && config.Config.AdminAllowLocalhost {
				next.ServeHTTP(w, r)
				return
			}

			writeForbidden(w, "admin access requires a Tailscale connection")
		})
	}
}

func writeForbidden(w http.ResponseWriter, message string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusForbidden)
	if err := json.NewEncoder(w).Encode(map[string]any{"message": message, "code": http.StatusForbidden}); err != nil {
		log.Error().Err(err).Msg("writeForbidden: failed to encode response")
	}
}

// ResolveWhois resolves the Tailscale identity for a request.
// It tries the context first (set by ProviderUserMiddleware on direct
// tsnet connections), then falls back to x-tsdproxy-* headers when the
// request originates from localhost (set by the in-process reverse proxy).
func ResolveWhois(r *http.Request) model.Whois {
	if who, ok := model.WhoisFromContext(r.Context()); ok && who.ID != "" {
		return who
	}

	if IsLocalhost(r.RemoteAddr) {
		return model.Whois{
			ID:            r.Header.Get(consts.HeaderID),
			Username:      r.Header.Get(consts.HeaderUsername),
			DisplayName:   r.Header.Get(consts.HeaderDisplayName),
			ProfilePicURL: r.Header.Get(consts.HeaderProfilePicURL),
		}
	}

	return model.Whois{}
}

func IsLocalhost(remoteAddr string) bool {
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
		if !IsLocalhost(r.RemoteAddr) {
			r.Header.Del(consts.HeaderID)
			r.Header.Del(consts.HeaderUsername)
			r.Header.Del(consts.HeaderDisplayName)
			r.Header.Del(consts.HeaderProfilePicURL)
		}
		next.ServeHTTP(w, r)
	})
}

func WhoAmIHandler(srv *HTTPServer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		who := ResolveWhois(r)
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
