// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package core

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rs/zerolog"

	"github.com/almeidapaulopt/tsdproxy/internal/config"
	"github.com/almeidapaulopt/tsdproxy/internal/consts"
	"github.com/almeidapaulopt/tsdproxy/internal/core/secretstring"
	"github.com/almeidapaulopt/tsdproxy/internal/model"
)

func saveConfig(t *testing.T) {
	t.Helper()
	origConfig := config.Config
	t.Cleanup(func() { config.Config = origConfig })
	config.SetTestConfig(t.TempDir(), "")
}

func saveProxyAuthToken(t *testing.T) {
	t.Helper()
	origToken := proxyAuthToken
	t.Cleanup(func() { proxyAuthToken = origToken })
}

func TestInitProxyAuth(t *testing.T) {
	saveProxyAuthToken(t)
	log := zerolog.Nop()

	if got := ProxyAuthToken(); got != "" {
		t.Errorf("ProxyAuthToken() before init = %q, want empty", got)
	}

	InitProxyAuth(log)
	token := ProxyAuthToken()
	if token == "" {
		t.Fatal("InitProxyAuth() generated empty token")
	}
	if len(token) != 64 {
		t.Errorf("InitProxyAuth() token length = %d, want 64", len(token))
	}

	oldToken := token
	InitProxyAuth(log)
	newToken := ProxyAuthToken()
	if newToken == oldToken {
		t.Error("InitProxyAuth() generated the same token on second call")
	}
}

func TestExtractBearerToken(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		auth string
		want string
	}{
		{name: "valid Bearer token", auth: "Bearer mytoken123", want: "mytoken123"},
		{name: "no auth header", auth: "", want: ""},
		{name: "wrong prefix Basic", auth: "Basic dXNlcjpwYXNz", want: ""},
		{name: "empty Bearer value", auth: "Bearer ", want: ""},
		{name: "case-insensitive Bearer prefix", auth: "bearer mytoken", want: "mytoken"},
		{name: "token with special chars", auth: "Bearer token-123_abc/xyz", want: "token-123_abc/xyz"},
		{name: "short auth header", auth: "Be", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			if tt.auth != "" {
				req.Header.Set("Authorization", tt.auth)
			}
			if got := extractBearerToken(req); got != tt.want {
				t.Errorf("extractBearerToken() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestValidAPIKey(t *testing.T) {
	saveConfig(t)

	tests := []struct {
		name       string
		apiKey     string
		authHeader string
		want       bool
	}{
		{name: "no API key configured", apiKey: "", authHeader: "Bearer some-key", want: false},
		{name: "valid Bearer token", apiKey: "my-api-key", authHeader: "Bearer my-api-key", want: true},
		{name: "wrong token", apiKey: "my-api-key", authHeader: "Bearer wrong-key", want: false},
		{name: "malformed auth header", apiKey: "my-api-key", authHeader: "Basic dXNlcjpwYXNz", want: false},
		{name: "empty auth header", apiKey: "my-api-key", authHeader: "", want: false},
		{name: "Bearer with empty token", apiKey: "my-api-key", authHeader: "Bearer ", want: false},
		{name: "NotBearer prefix", apiKey: "my-api-key", authHeader: "NotBearer my-api-key", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			saveConfig(t)
			config.Config.APIKey = secretstring.SecretString(tt.apiKey)
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			if tt.authHeader != "" {
				req.Header.Set("Authorization", tt.authHeader)
			}
			if got := ValidAPIKey(req); got != tt.want {
				t.Errorf("ValidAPIKey() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestUserIDIsAdmin(t *testing.T) {
	saveConfig(t)

	tests := []struct {
		name                string
		id                  string
		admins              []string
		adminAllowLocalhost bool
		want                bool
	}{
		{name: "empty admins list", admins: nil, id: "anyuser@ts.com", want: true},
		{name: "user in admins list", admins: []string{"admin@ts.com"}, id: "admin@ts.com", want: true},
		{name: "user not in admins list", admins: []string{"admin@ts.com"}, id: "other@ts.com", want: false},
		{name: "__localhost__ in populated admins with allow", admins: []string{"admin@ts.com"}, adminAllowLocalhost: true, id: "__localhost__", want: true},
		{name: "__localhost__ in populated admins without allow", admins: []string{"admin@ts.com"}, adminAllowLocalhost: false, id: "__localhost__", want: false},
		{name: "empty admins, localhost id", admins: nil, id: "__localhost__", want: true},
		{name: "empty string id", admins: []string{"admin@ts.com"}, id: "", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			saveConfig(t)
			config.Config.Admins = tt.admins
			config.Config.AdminAllowLocalhost = tt.adminAllowLocalhost
			if got := UserIDIsAdmin(tt.id); got != tt.want {
				t.Errorf("UserIDIsAdmin(%q) = %v, want %v", tt.id, got, tt.want)
			}
		})
	}
}

func TestIsAdmin(t *testing.T) {
	saveConfig(t)

	tests := []struct {
		name                string
		apiKey              string
		requestAPIKey       string
		remoteAddr          string
		whoisID             string
		admins              []string
		adminAllowLocalhost bool
		want                bool
	}{
		{name: "valid API key", apiKey: "secret", requestAPIKey: "secret", whoisID: "other@ts.com", want: true},
		{name: "invalid API key with admin Whois", apiKey: "secret", requestAPIKey: "wrong", admins: []string{"admin@ts.com"}, whoisID: "admin@ts.com", want: true},
		{name: "invalid API key non-admin Whois", apiKey: "secret", requestAPIKey: "wrong", admins: []string{"admin@ts.com"}, whoisID: "other@ts.com", want: false},
		{name: "Whois admin in populated admins", admins: []string{"admin@ts.com"}, whoisID: "admin@ts.com", want: true},
		{name: "Whois non-admin in populated admins", admins: []string{"admin@ts.com"}, whoisID: "other@ts.com", want: false},
		{name: "Whois any user with empty admins", whoisID: "anyuser@ts.com", want: true},
		{name: "localhost with AdminAllowLocalhost", adminAllowLocalhost: true, remoteAddr: "127.0.0.1:12345", want: true},
		{name: "localhost without AdminAllowLocalhost", adminAllowLocalhost: false, remoteAddr: "127.0.0.1:12345", want: false},
		{name: "non-localhost no identity", remoteAddr: "8.8.8.8:8080", want: false},
		{name: "non-localhost with AdminAllowLocalhost", adminAllowLocalhost: true, remoteAddr: "8.8.8.8:8080", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			saveConfig(t)
			config.Config.Admins = tt.admins
			config.Config.AdminAllowLocalhost = tt.adminAllowLocalhost
			config.Config.APIKey = secretstring.SecretString(tt.apiKey)

			req := httptest.NewRequest(http.MethodGet, "/", nil)
			if tt.requestAPIKey != "" {
				req.Header.Set("Authorization", "Bearer "+tt.requestAPIKey)
			}
			if tt.remoteAddr != "" {
				req.RemoteAddr = tt.remoteAddr
			}
			if tt.whoisID != "" {
				ctx := model.WhoisNewContext(req.Context(), model.Whois{ID: tt.whoisID})
				req = req.WithContext(ctx)
			}

			if got := IsAdmin(req); got != tt.want {
				t.Errorf("IsAdmin() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAdminMiddleware(t *testing.T) {
	saveConfig(t)

	tests := []struct {
		name                string
		apiKey              string
		requestAPIKey       string
		remoteAddr          string
		whoisID             string
		admins              []string
		wantStatus          int
		adminAllowLocalhost bool
	}{
		{name: "valid API key bypasses admin check", apiKey: "secret", requestAPIKey: "secret", wantStatus: http.StatusOK},
		{name: "invalid API key admin Whois", apiKey: "secret", requestAPIKey: "wrong", admins: []string{"admin@ts.com"}, whoisID: "admin@ts.com", wantStatus: http.StatusOK},
		{name: "invalid API key non-admin Whois", apiKey: "secret", requestAPIKey: "wrong", admins: []string{"admin@ts.com"}, whoisID: "other@ts.com", wantStatus: http.StatusForbidden},
		{name: "Whois admin in populated admins", admins: []string{"admin@ts.com"}, whoisID: "admin@ts.com", wantStatus: http.StatusOK},
		{name: "Whois non-admin in populated admins", admins: []string{"admin@ts.com"}, whoisID: "other@ts.com", wantStatus: http.StatusForbidden},
		{name: "Whois any user with empty admins", whoisID: "anyuser@ts.com", wantStatus: http.StatusOK},
		{name: "localhost with AdminAllowLocalhost", adminAllowLocalhost: true, remoteAddr: "127.0.0.1:12345", wantStatus: http.StatusOK},
		{name: "localhost without AdminAllowLocalhost", adminAllowLocalhost: false, remoteAddr: "127.0.0.1:12345", wantStatus: http.StatusForbidden},
		{name: "non-localhost no identity", remoteAddr: "8.8.8.8:8080", wantStatus: http.StatusForbidden},
		{name: "admins populated, no Whois, localhost allowed", admins: []string{"admin@ts.com"}, adminAllowLocalhost: true, remoteAddr: "127.0.0.1:12345", wantStatus: http.StatusOK},
		{name: "admins populated, no Whois, localhost not allowed", admins: []string{"admin@ts.com"}, adminAllowLocalhost: false, remoteAddr: "127.0.0.1:12345", wantStatus: http.StatusForbidden},
		// Docker bridge IPs (172.16.0.0/12) hit the middleware when Docker
		// port-maps to tsdproxy — they must be treated as trusted when
		// AdminAllowLocalhost is true, but still rejected otherwise.
		{name: "Docker bridge with AdminAllowLocalhost", adminAllowLocalhost: true, remoteAddr: "172.17.0.1:8080", wantStatus: http.StatusOK},
		{name: "Docker bridge without AdminAllowLocalhost", adminAllowLocalhost: false, remoteAddr: "172.17.0.1:8080", wantStatus: http.StatusForbidden},
		{name: "Docker bridge 172.31.x with AdminAllowLocalhost", adminAllowLocalhost: true, remoteAddr: "172.31.255.1:8080", wantStatus: http.StatusOK},
		// CGNAT IPs (100.64.0.0/10) cover the case where a LAN client runs
		// Tailscale, so the source IP appears as a Tailscale address even
		// when connecting to the host's LAN IP.
		{name: "CGNAT with AdminAllowLocalhost", adminAllowLocalhost: true, remoteAddr: "100.78.209.50:8080", wantStatus: http.StatusOK},
		{name: "CGNAT without AdminAllowLocalhost", adminAllowLocalhost: false, remoteAddr: "100.78.209.50:8080", wantStatus: http.StatusForbidden},
		{name: "admins populated, Docker bridge allowed", admins: []string{"admin@ts.com"}, adminAllowLocalhost: true, remoteAddr: "172.17.0.1:8080", wantStatus: http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			saveConfig(t)
			config.Config.Admins = tt.admins
			config.Config.AdminAllowLocalhost = tt.adminAllowLocalhost
			config.Config.APIKey = secretstring.SecretString(tt.apiKey)

			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			if tt.requestAPIKey != "" {
				req.Header.Set("Authorization", "Bearer "+tt.requestAPIKey)
			}
			if tt.remoteAddr != "" {
				req.RemoteAddr = tt.remoteAddr
			}
			if tt.whoisID != "" {
				ctx := model.WhoisNewContext(req.Context(), model.Whois{ID: tt.whoisID})
				req = req.WithContext(ctx)
			}

			handler := AdminMiddleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))
			handler.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("AdminMiddleware() status = %d, want %d", rec.Code, tt.wantStatus)
			}
		})
	}
}

func TestViewerMiddleware(t *testing.T) {
	saveConfig(t)

	tests := []struct {
		name                string
		apiKey              string
		requestAPIKey       string
		remoteAddr          string
		whoisID             string
		wantStatus          int
		adminAllowLocalhost bool
	}{
		{name: "valid API key", apiKey: "secret", requestAPIKey: "secret", remoteAddr: "8.8.8.8", wantStatus: http.StatusOK},
		{name: "invalid API key no Whois", apiKey: "secret", requestAPIKey: "wrong", remoteAddr: "8.8.8.8", wantStatus: http.StatusForbidden},
		{name: "Whois with ID", whoisID: "anyuser@ts.com", remoteAddr: "8.8.8.8", wantStatus: http.StatusOK},
		{name: "localhost with AdminAllowLocalhost", adminAllowLocalhost: true, remoteAddr: "127.0.0.1:12345", wantStatus: http.StatusOK},
		{name: "localhost without AdminAllowLocalhost", adminAllowLocalhost: false, remoteAddr: "127.0.0.1:12345", wantStatus: http.StatusForbidden},
		{name: "no identity not localhost", remoteAddr: "8.8.8.8:8080", wantStatus: http.StatusForbidden},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			saveConfig(t)
			config.Config.AdminAllowLocalhost = tt.adminAllowLocalhost
			config.Config.APIKey = secretstring.SecretString(tt.apiKey)

			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			if tt.requestAPIKey != "" {
				req.Header.Set("Authorization", "Bearer "+tt.requestAPIKey)
			}
			if tt.remoteAddr != "" {
				req.RemoteAddr = tt.remoteAddr
			}
			if tt.whoisID != "" {
				ctx := model.WhoisNewContext(req.Context(), model.Whois{ID: tt.whoisID})
				req = req.WithContext(ctx)
			}

			handler := ViewerMiddleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))
			handler.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("ViewerMiddleware() status = %d, want %d", rec.Code, tt.wantStatus)
			}
		})
	}
}

func TestWriteForbidden(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		accept string
		wantCT string
	}{
		{name: "HTML Accept returns HTML", accept: "text/html", wantCT: "text/html; charset=utf-8"},
		{name: "JSON Accept returns JSON", accept: "application/json", wantCT: "application/json; charset=utf-8"},
		{name: "no Accept returns JSON", accept: "", wantCT: "application/json; charset=utf-8"},
		{name: "wildcard Accept returns JSON", accept: "*/*", wantCT: "application/json; charset=utf-8"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			if tt.accept != "" {
				req.Header.Set("Accept", tt.accept)
			}

			writeForbidden(rec, req, "access denied")

			if rec.Code != http.StatusForbidden {
				t.Errorf("writeForbidden() status = %d, want %d", rec.Code, http.StatusForbidden)
			}

			ct := rec.Header().Get("Content-Type")
			if ct != tt.wantCT {
				t.Errorf("writeForbidden() Content-Type = %q, want %q", ct, tt.wantCT)
			}

			body := rec.Body.String()

			if strings.Contains(tt.wantCT, "text/html") {
				if !strings.Contains(body, "access denied") {
					t.Error("writeForbidden() HTML body missing message")
				}
				if !strings.Contains(body, "<html") && !strings.Contains(body, "<!DOCTYPE") {
					t.Error("writeForbidden() HTML body missing HTML markup")
				}
			} else {
				var resp map[string]any
				if err := json.Unmarshal([]byte(body), &resp); err != nil {
					t.Fatalf("writeForbidden() JSON body decode error: %v", err)
				}
				if msg, _ := resp["message"].(string); msg != "access denied" {
					t.Errorf("writeForbidden() JSON message = %q, want %q", msg, "access denied")
				}
				if code, _ := resp["code"].(float64); int(code) != http.StatusForbidden {
					t.Errorf("writeForbidden() JSON code = %v, want %d", code, http.StatusForbidden)
				}
			}
		})
	}
}

func TestResolveWhois(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name              string
		remoteAddr        string
		whoisCtxID        string
		setEmptyWhois     bool
		headers           map[string]string
		wantID            string
		wantUsername      string
		wantDisplayName   string
		wantProfilePicURL string
	}{
		{
			name:       "Whois in context takes priority over localhost headers",
			remoteAddr: "127.0.0.1:12345",
			whoisCtxID: "ctx-user@ts.com",
			headers:    map[string]string{consts.HeaderID: "header-user@ts.com"},
			wantID:     "ctx-user@ts.com",
		},
		{
			name:       "localhost with headers returns Whois",
			remoteAddr: "127.0.0.1:12345",
			headers: map[string]string{
				consts.HeaderID:            "user@ts.com",
				consts.HeaderUsername:      "user",
				consts.HeaderDisplayName:   "User",
				consts.HeaderProfilePicURL: "https://example.com/pic",
			},
			wantID:            "user@ts.com",
			wantUsername:      "user",
			wantDisplayName:   "User",
			wantProfilePicURL: "https://example.com/pic",
		},
		{
			name:       "no identity",
			remoteAddr: "8.8.8.8:8080",
			wantID:     "",
		},
		{
			name:       "localhost without headers returns empty",
			remoteAddr: "127.0.0.1:12345",
			wantID:     "",
		},
		{
			name:       "not localhost with headers returns empty",
			remoteAddr: "8.8.8.8:8080",
			headers:    map[string]string{consts.HeaderID: "user@ts.com"},
			wantID:     "",
		},
		{
			name:          "context Whois with empty ID falls through",
			remoteAddr:    "127.0.0.1:12345",
			whoisCtxID:    "",
			setEmptyWhois: true,
			headers: map[string]string{
				consts.HeaderID: "header-user@ts.com",
			},
			wantID: "header-user@ts.com",
		},
		{
			name:       "localhost with only ID header propagates ID only",
			remoteAddr: "127.0.0.1:12345",
			headers: map[string]string{
				consts.HeaderID: "lonely@ts.com",
			},
			wantID: "lonely@ts.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.RemoteAddr = tt.remoteAddr
			for k, v := range tt.headers {
				req.Header.Set(k, v)
			}
			if tt.whoisCtxID != "" || tt.setEmptyWhois {
				who := model.Whois{ID: tt.whoisCtxID}
				ctx := model.WhoisNewContext(req.Context(), who)
				req = req.WithContext(ctx)
			}

			got := ResolveWhois(req)
			if got.ID != tt.wantID {
				t.Errorf("ResolveWhois().ID = %q, want %q", got.ID, tt.wantID)
			}
			if got.Username != tt.wantUsername {
				t.Errorf("ResolveWhois().Username = %q, want %q", got.Username, tt.wantUsername)
			}
			if got.DisplayName != tt.wantDisplayName {
				t.Errorf("ResolveWhois().DisplayName = %q, want %q", got.DisplayName, tt.wantDisplayName)
			}
			if got.ProfilePicURL != tt.wantProfilePicURL {
				t.Errorf("ResolveWhois().ProfilePicURL = %q, want %q", got.ProfilePicURL, tt.wantProfilePicURL)
			}
		})
	}
}

func TestIsTrustedSource_Edge(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		remoteAddr string
		want       bool
	}{
		{name: "private 10.0.0.0/8 exact start", remoteAddr: "10.0.0.0:80", want: true},
		{name: "private 10.0.0.0/8 exact end", remoteAddr: "10.255.255.255:80", want: true},
		{name: "CGNAT 100.64.0.0/10 exact start", remoteAddr: "100.64.0.0:80", want: true},
		{name: "CGNAT 100.64.0.0/10 exact end", remoteAddr: "100.127.255.255:80", want: true},
		{name: "link-local 169.254.x.x", remoteAddr: "169.254.1.1:80", want: false},
		{name: "IPv6 loopback", remoteAddr: "[::1]:443", want: true},
		{name: "IPv4-mapped IPv6 loopback", remoteAddr: "[::ffff:127.0.0.1]:80", want: true},
		{name: "invalid IP", remoteAddr: "not-an-ip:80", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := IsTrustedSource(tt.remoteAddr); got != tt.want {
				t.Errorf("IsTrustedSource(%q) = %v, want %v", tt.remoteAddr, got, tt.want)
			}
		})
	}
}

func TestIsCGNAT(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		ip   string
		want bool
	}{
		{name: "100.64.0.0 start of range", ip: "100.64.0.0", want: true},
		{name: "100.64.0.1 typical", ip: "100.64.0.1", want: true},
		{name: "100.127.255.255 end of range", ip: "100.127.255.255", want: true},
		{name: "100.78.209.50 Tailscale typical", ip: "100.78.209.50", want: true},
		{name: "100.63.255.255 just below range", ip: "100.63.255.255", want: false},
		{name: "100.128.0.1 just above range", ip: "100.128.0.1", want: false},
		{name: "loopback not CGNAT", ip: "127.0.0.1", want: false},
		{name: "private 10.x not CGNAT", ip: "10.0.0.1", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ip := net.ParseIP(tt.ip)
			if ip == nil {
				t.Fatalf("invalid IP: %s", tt.ip)
			}
			if got := isCGNAT(ip); got != tt.want {
				t.Errorf("isCGNAT(%s) = %v, want %v", tt.ip, got, tt.want)
			}
		})
	}
}

func TestValidProxyAuthToken(t *testing.T) {
	saveProxyAuthToken(t)

	tests := []struct {
		name              string
		remoteAddr        string
		proxyAuthTokenVal string
		authHeaderVal     string
		want              bool
	}{
		{name: "localhost valid token", remoteAddr: "127.0.0.1:12345", proxyAuthTokenVal: "secret-token", authHeaderVal: "secret-token", want: true},
		{name: "localhost wrong token", remoteAddr: "127.0.0.1:12345", proxyAuthTokenVal: "secret-token", authHeaderVal: "wrong-token", want: false},
		{name: "non-localhost valid token", remoteAddr: "8.8.8.8:8080", proxyAuthTokenVal: "secret-token", authHeaderVal: "secret-token", want: false},
		{name: "localhost no auth header", remoteAddr: "127.0.0.1:12345", proxyAuthTokenVal: "secret-token", authHeaderVal: "", want: false},
		{name: "localhost empty proxyAuthToken", remoteAddr: "127.0.0.1:12345", proxyAuthTokenVal: "", authHeaderVal: "secret-token", want: false},
		{name: "non-localhost empty proxyAuthToken", remoteAddr: "8.8.8.8:8080", proxyAuthTokenVal: "", authHeaderVal: "", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			saveProxyAuthToken(t)
			proxyAuthToken = tt.proxyAuthTokenVal
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.RemoteAddr = tt.remoteAddr
			if tt.authHeaderVal != "" {
				req.Header.Set(consts.HeaderAuthToken, tt.authHeaderVal)
			}

			if got := validProxyAuthToken(req); got != tt.want {
				t.Errorf("validProxyAuthToken() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestStripProxyIdentityHeaders(t *testing.T) {
	saveProxyAuthToken(t)
	proxyAuthToken = "valid-secret"

	tests := []struct {
		identityHeaders map[string]string
		name            string
		remoteAddr      string
		authToken       string
		wantPreserved   bool
	}{
		{
			name:       "valid token from localhost preserves identity headers",
			remoteAddr: "127.0.0.1:12345",
			authToken:  "valid-secret",
			identityHeaders: map[string]string{
				consts.HeaderID:            "user@ts.com",
				consts.HeaderUsername:      "user",
				consts.HeaderDisplayName:   "User",
				consts.HeaderProfilePicURL: "https://example.com/pic",
			},
			wantPreserved: true,
		},
		{
			name:       "wrong token from localhost strips identity headers",
			remoteAddr: "127.0.0.1:12345",
			authToken:  "wrong-secret",
			identityHeaders: map[string]string{
				consts.HeaderID: "user@ts.com",
			},
			wantPreserved: false,
		},
		{
			name:       "valid token from non-localhost strips identity headers",
			remoteAddr: "8.8.8.8:8080",
			authToken:  "valid-secret",
			identityHeaders: map[string]string{
				consts.HeaderID: "user@ts.com",
			},
			wantPreserved: false,
		},
		{
			name:       "no auth token from localhost strips identity headers",
			remoteAddr: "127.0.0.1:12345",
			authToken:  "",
			identityHeaders: map[string]string{
				consts.HeaderID: "user@ts.com",
			},
			wantPreserved: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.RemoteAddr = tt.remoteAddr
			req.Header.Set(consts.HeaderAuthToken, tt.authToken)
			for k, v := range tt.identityHeaders {
				req.Header.Set(k, v)
			}

			var headersAfter http.Header
			handler := StripProxyIdentityHeaders(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				headersAfter = r.Header.Clone()
				w.WriteHeader(http.StatusOK)
			}))
			handler.ServeHTTP(rec, req)

			if headersAfter.Get(consts.HeaderAuthToken) != "" {
				t.Error("StripProxyIdentityHeaders did not strip auth token header")
			}

			for k := range tt.identityHeaders {
				got := headersAfter.Get(k)
				if tt.wantPreserved && got == "" {
					t.Errorf("StripProxyIdentityHeaders stripped identity header %q, want preserved", k)
				}
				if !tt.wantPreserved && got != "" {
					t.Errorf("StripProxyIdentityHeaders preserved identity header %q, want stripped", k)
				}
			}

			if rec.Code != http.StatusOK {
				t.Errorf("StripProxyIdentityHeaders status = %d, want %d", rec.Code, http.StatusOK)
			}
		})
	}
}

func TestWhoAmIHandler(t *testing.T) {
	t.Parallel()

	t.Run("with Whois identity returns 200", func(t *testing.T) {
		srv := NewHTTPServer(zerolog.Nop())
		handler := WhoAmIHandler(srv)

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		ctx := model.WhoisNewContext(req.Context(), model.Whois{
			ID:            "user@ts.com",
			Username:      "myusername",
			DisplayName:   "My Name",
			ProfilePicURL: "https://example.com/avatar.png",
		})
		req = req.WithContext(ctx)

		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("WhoAmIHandler status = %d, want %d", rec.Code, http.StatusOK)
		}

		ct := rec.Header().Get("Content-Type")
		if !strings.Contains(ct, "application/json") {
			t.Errorf("WhoAmIHandler Content-Type = %q, want application/json", ct)
		}

		var body map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("WhoAmIHandler JSON decode error: %v", err)
		}
		if id, _ := body["id"].(string); id != "user@ts.com" {
			t.Errorf("WhoAmIHandler id = %q, want %q", id, "user@ts.com")
		}
		if un, _ := body["username"].(string); un != "myusername" {
			t.Errorf("WhoAmIHandler username = %q, want %q", un, "myusername")
		}
		if dn, _ := body["displayName"].(string); dn != "My Name" {
			t.Errorf("WhoAmIHandler displayName = %q, want %q", dn, "My Name")
		}
		if pu, _ := body["profilePicUrl"].(string); pu != "https://example.com/avatar.png" {
			t.Errorf("WhoAmIHandler profilePicUrl = %q, want %q", pu, "https://example.com/avatar.png")
		}
	})

	t.Run("without identity returns 401", func(t *testing.T) {
		srv := NewHTTPServer(zerolog.Nop())
		handler := WhoAmIHandler(srv)

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/", nil)

		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Errorf("WhoAmIHandler status = %d, want %d", rec.Code, http.StatusUnauthorized)
		}

		var body map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("WhoAmIHandler JSON decode error: %v", err)
		}
		if msg, _ := body["message"].(string); msg != "no Tailscale identity found" {
			t.Errorf("WhoAmIHandler message = %q, want %q", msg, "no Tailscale identity found")
		}
		if code, _ := body["code"].(float64); int(code) != http.StatusUnauthorized {
			t.Errorf("WhoAmIHandler code = %v, want %d", code, http.StatusUnauthorized)
		}
	})
}

func TestIsLocalhost(t *testing.T) {
	tests := []struct {
		name       string
		remoteAddr string
		want       bool
	}{
		{"IPv4 loopback", "127.0.0.1:12345", true},
		{"IPv4 loopback other", "127.0.0.2:8080", true},
		{"IPv6 loopback", "[::1]:8080", true},
		{"Docker bridge", "172.17.0.1:8080", false},
		{"Private 10.x", "10.0.0.1:8080", false},
		{"Private 192.168.x", "192.168.1.1:8080", false},
		{"Public IP", "8.8.8.8:8080", false},
		{"no port loopback", "127.0.0.1", true},
		{"no port docker bridge", "172.17.0.1", false},
		{"hostname", "example.com:8080", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := model.IsLocalhost(tt.remoteAddr); got != tt.want {
				t.Errorf("IsLocalhost(%q) = %v, want %v", tt.remoteAddr, got, tt.want)
			}
		})
	}
}

func TestIsTrustedSource(t *testing.T) {
	tests := []struct {
		name       string
		remoteAddr string
		want       bool
	}{
		{"IPv4 loopback", "127.0.0.1:12345", true},
		{"IPv4 loopback other", "127.0.0.2:8080", true},
		{"IPv6 loopback", "[::1]:8080", true},
		{"Docker bridge default", "172.17.0.1:8080", true},
		{"Docker bridge 172.16.x", "172.16.0.1:8080", true},
		{"Docker bridge 172.31.x", "172.31.255.1:8080", true},
		{"Private 10.x", "10.0.0.1:8080", true},
		{"Private 10.255.255.255", "10.255.255.255:8080", true},
		{"Private 192.168.x", "192.168.1.1:8080", true},
		{"Private 192.168.0.255", "192.168.0.255:8080", true},
		{"Public 8.8.8.8", "8.8.8.8:8080", false},
		{"Public 1.1.1.1", "1.1.1.1:8080", false},
		{"CGNAT 100.64.0.0", "100.64.0.1:8080", true},
		{"CGNAT 100.78.209.x (Tailscale)", "100.78.209.50:8080", true},
		{"CGNAT 100.127.255.255", "100.127.255.255:8080", true},
		{"100.63.x below CGNAT", "100.63.255.255:8080", false},
		{"100.128.x above CGNAT", "100.128.0.1:8080", false},
		{"172.32.x not private", "172.32.0.1:8080", false},
		{"172.15.x not private", "172.15.0.1:8080", false},
		{"no port loopback", "127.0.0.1", true},
		{"no port docker bridge", "172.17.0.1", true},
		{"no port CGNAT", "100.78.209.50", true},
		{"no port public", "8.8.8.8", false},
		{"hostname", "example.com:8080", false},
		{"empty string", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsTrustedSource(tt.remoteAddr); got != tt.want {
				t.Errorf("IsTrustedSource(%q) = %v, want %v", tt.remoteAddr, got, tt.want)
			}
		})
	}
}
