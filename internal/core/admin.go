// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package core

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"net"
	"net/http"
	"slices"
	"strings"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"github.com/almeidapaulopt/tsdproxy/internal/config"
	"github.com/almeidapaulopt/tsdproxy/internal/consts"
	"github.com/almeidapaulopt/tsdproxy/internal/model"
	"github.com/almeidapaulopt/tsdproxy/internal/ui/pages"
)

var proxyAuthToken string

// InitProxyAuth generates the per-process secret used to authenticate
// identity headers forwarded by the internal reverse proxy. Must be
// called once during startup, before any HTTP handlers are registered.
func InitProxyAuth(log zerolog.Logger) {
	b := make([]byte, 32) //nolint:mnd
	if _, err := rand.Read(b); err != nil {
		log.Fatal().Err(err).Msg("failed to generate proxy auth token")
	}
	proxyAuthToken = hex.EncodeToString(b)
}

// ProxyAuthToken returns the per-process secret set by InitProxyAuth.
func ProxyAuthToken() string { return proxyAuthToken }

// AdminMiddleware authenticates requests to admin-only endpoints.
//
// Access is granted in priority order:
//  1. Valid API key via Authorization: Bearer <token>
//  2. Tailscale identity in config.Config.Admins list
//  3. Localhost + AdminAllowLocalhost
//
// When config.Config.Admins is empty, any authenticated Tailscale user
// is considered an admin. Use ViewerMiddleware for read-only access
// that allows all Tailscale users regardless of the admins list.
func AdminMiddleware() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if ValidAPIKey(r) {
				next.ServeHTTP(w, r)
				return
			}

			id := ResolveWhois(r).ID

			admins := config.Config.Admins
			if len(admins) > 0 {
				if id != "" && slices.Contains(admins, id) {
					next.ServeHTTP(w, r)
					return
				}
				if id != "" {
					writeForbidden(w, r, "admin access required")
					return
				}

				if IsTrustedSource(r.RemoteAddr) && config.Config.AdminAllowLocalhost {
					next.ServeHTTP(w, r)
					return
				}

				writeForbidden(w, r, "admin access requires a Tailscale connection")
				return
			}

			if id != "" {
				next.ServeHTTP(w, r)
				return
			}

			if IsTrustedSource(r.RemoteAddr) && config.Config.AdminAllowLocalhost {
				next.ServeHTTP(w, r)
				return
			}

			writeForbidden(w, r, "admin access requires a Tailscale connection")
		})
	}
}

// ViewerMiddleware authenticates requests to read-only dashboard endpoints.
// Any authenticated Tailscale user is allowed, regardless of the admins list.
// Use AdminMiddleware for endpoints that require admin privileges.
func ViewerMiddleware() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if ValidAPIKey(r) {
				next.ServeHTTP(w, r)
				return
			}

			id := ResolveWhois(r).ID
			if id != "" {
				next.ServeHTTP(w, r)
				return
			}

			if IsTrustedSource(r.RemoteAddr) && config.Config.AdminAllowLocalhost {
				next.ServeHTTP(w, r)
				return
			}

			writeForbidden(w, r, "dashboard access requires a Tailscale connection")
		})
	}
}

// IsAdmin checks whether the authenticated user for the given request
// has admin privileges. Returns true when config.Config.Admins is empty
// (all users are admins) or when the user's Tailscale ID is in the list.
func IsAdmin(r *http.Request) bool {
	if ValidAPIKey(r) {
		return true
	}
	id := ResolveWhois(r).ID
	if id != "" {
		return UserIDIsAdmin(id)
	}
	return IsTrustedSource(r.RemoteAddr) && config.Config.AdminAllowLocalhost
}

func UserIDIsAdmin(id string) bool {
	admins := config.Config.Admins
	if len(admins) == 0 {
		return true
	}
	if id == "__localhost__" {
		return config.Config.AdminAllowLocalhost
	}
	return slices.Contains(admins, id)
}

func ValidAPIKey(r *http.Request) bool {
	key := config.Config.APIKey
	if key == "" {
		return false
	}
	token := extractBearerToken(r)
	return subtle.ConstantTimeCompare([]byte(token), []byte(key)) == 1
}

func extractBearerToken(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	if len(auth) > 7 && strings.EqualFold(auth[:7], "Bearer ") {
		return auth[7:]
	}
	return ""
}

func writeForbidden(w http.ResponseWriter, r *http.Request, message string) {
	accept := r.Header.Get("Accept")
	if strings.Contains(accept, "text/html") {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusForbidden)
		_ = pages.ForbiddenPage(message).Render(r.Context(), w)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusForbidden)
	if err := json.NewEncoder(w).Encode(map[string]any{"message": message, "code": http.StatusForbidden}); err != nil {
		log.Error().Err(err).Msg("writeForbidden: failed to encode response")
	}
}

// ResolveWhois resolves the Tailscale identity for a request.
// Priority: request context (set by ProviderUserMiddleware on direct
// tsnet connections), then x-tsdproxy-* headers from localhost (set by
// the internal reverse proxy, validated via per-process auth token in
// StripProxyIdentityHeaders).
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

// IsTrustedSource returns true when the request originates from a
// trusted network: loopback (127.0.0.0/8, ::1), RFC 1918 private
// addresses (172.16.0.0/12, 10.0.0.0/8, 192.168.0.0/16), or RFC 6598
// Carrier-Grade NAT (100.64.0.0/10) used by Tailscale.
//
// The private-network check extends the loopback-only IsLocalhost to
// cover Docker port-mapped requests, which arrive inside the container
// from the Docker bridge gateway (e.g. 172.17.0.1) rather than
// 127.0.0.1. The CGNAT check covers the case where a client on the LAN
// has Tailscale running, causing the source IP to appear as a Tailscale
// address (100.x.x.x) even when connecting to the host's LAN IP.
//
// IMPORTANT: This must NOT be used where loopback-only trust is
// required (e.g. validating proxy auth tokens or identity headers).
// Use IsLocalhost for those cases.
func IsTrustedSource(remoteAddr string) bool {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}

	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}

	return ip.IsLoopback() || isCGNAT(ip) || ip.IsPrivate()
}

// isCGNAT returns true for RFC 6598 Carrier-Grade NAT addresses
// (100.64.0.0/10), which Tailscale uses for its mesh address space.
func isCGNAT(ip net.IP) bool {
	_, cgnat, _ := net.ParseCIDR("100.64.0.0/10")
	return cgnat.Contains(ip)
}

// validProxyAuthToken checks whether the request carries a valid per-process
// auth token from localhost. Returns false when the token is uninitialised
// (fail-closed) and uses constant-time comparison.
func validProxyAuthToken(r *http.Request) bool {
	if !IsLocalhost(r.RemoteAddr) {
		return false
	}
	if proxyAuthToken == "" {
		return false
	}
	token := r.Header.Get(consts.HeaderAuthToken)
	return subtle.ConstantTimeCompare([]byte(token), []byte(proxyAuthToken)) == 1
}

// StripProxyIdentityHeaders removes x-tsdproxy-* identity and auth-token
// headers from incoming requests. Identity headers are preserved only when
// the request carries the correct per-process auth token and originates from
// localhost (i.e. from the internal reverse proxy forwarding an authenticated
// Tailscale session to the management listener). The auth token itself is
// always stripped to prevent leakage.
func StripProxyIdentityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		valid := validProxyAuthToken(r)
		// Always strip the auth token immediately so downstream handlers
		// never see the secret, even if they log or reflect headers.
		r.Header.Del(consts.HeaderAuthToken)

		if valid {
			next.ServeHTTP(w, r)
			return
		}

		r.Header.Del(consts.HeaderID)
		r.Header.Del(consts.HeaderUsername)
		r.Header.Del(consts.HeaderDisplayName)
		r.Header.Del(consts.HeaderProfilePicURL)
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
