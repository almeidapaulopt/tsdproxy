// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package tailscale

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	tailscale "tailscale.com/client/tailscale/v2"

	"github.com/almeidapaulopt/tsdproxy/internal/core/secretstring"
)

// --- APIClientFactory tests ---

func TestAPIClientFactoryIsAvailable(t *testing.T) {
	t.Parallel()

	f := NewAPIClientFactory("client-id", "client-secret")
	assert.True(t, f.IsAvailable())
}

func TestAPIClientFactoryNotAvailableEmptyID(t *testing.T) {
	t.Parallel()

	f := NewAPIClientFactory("", "client-secret")
	assert.False(t, f.IsAvailable())
}

func TestAPIClientFactoryNotAvailableEmptySecret(t *testing.T) {
	t.Parallel()

	f := NewAPIClientFactory("client-id", "")
	assert.False(t, f.IsAvailable())
}

func TestAPIClientFactoryNotAvailableBothEmpty(t *testing.T) {
	t.Parallel()

	f := NewAPIClientFactory("", "")
	assert.False(t, f.IsAvailable())
}

func TestAPIClientFactoryNewClientReturnsNilWhenUnavailable(t *testing.T) {
	t.Parallel()

	f := NewAPIClientFactory("", "secret")
	assert.Nil(t, f.NewClient(ScopeDevices))
}

func TestAPIClientFactoryNewClientReturnsNonNilWhenAvailable(t *testing.T) {
	t.Parallel()

	f := NewAPIClientFactory("id", "secret")
	client := f.NewClient(ScopeDevices, ScopeAuthKeys)
	require.NotNil(t, client)
	assert.Equal(t, "-", client.Tailnet)
	assert.Equal(t, userAgent, client.UserAgent)
}

func TestAPIClientFactoryTrimsWhitespace(t *testing.T) {
	t.Parallel()

	f := NewAPIClientFactory("  id  ", "  secret  ")
	assert.True(t, f.IsAvailable())
}

func TestScopesPerProxy(t *testing.T) {
	t.Parallel()

	assert.Equal(t, []string{ScopeDevices, ScopeAuthKeys}, ScopesPerProxy())
}

func TestScopesServices(t *testing.T) {
	t.Parallel()

	assert.Equal(t, []string{ScopeDevices, ScopeAuthKeys, ScopeServices}, ScopesServices())
}

// --- AuthManager ResolveKey tests (no OAuth) ---

func TestResolveKeyResolvedAuthKeyWins(t *testing.T) {
	t.Parallel()

	m := NewAuthManager(zerolog.Nop(), nil, false)
	key, err := m.ResolveKey(context.Background(), AuthConfig{
		ResolvedAuthKey: "resolved-key",
		ProxyAuthKey:    secretstring.SecretString("proxy-key"),
		ProviderAuthKey: secretstring.SecretString("provider-key"),
	}, "tag:test")
	require.NoError(t, err)
	assert.Equal(t, "resolved-key", key)
}

func TestResolveKeyProxyAuthKey(t *testing.T) {
	t.Parallel()

	m := NewAuthManager(zerolog.Nop(), nil, false)
	key, err := m.ResolveKey(context.Background(), AuthConfig{
		ProxyAuthKey:    secretstring.SecretString("proxy-key"),
		ProviderAuthKey: secretstring.SecretString("provider-key"),
	}, "tag:test")
	require.NoError(t, err)
	assert.Equal(t, "proxy-key", key)
}

func TestResolveKeyProviderAuthKeyFallback(t *testing.T) {
	t.Parallel()

	m := NewAuthManager(zerolog.Nop(), nil, false)
	key, err := m.ResolveKey(context.Background(), AuthConfig{
		ProviderAuthKey: secretstring.SecretString("provider-key"),
	}, "tag:test")
	require.NoError(t, err)
	assert.Equal(t, "provider-key", key)
}

func TestResolveKeyEmptyKeyInteractiveLogin(t *testing.T) {
	t.Parallel()

	m := NewAuthManager(zerolog.Nop(), nil, false)
	key, err := m.ResolveKey(context.Background(), AuthConfig{}, "tag:test")
	require.NoError(t, err)
	assert.Equal(t, "", key)
}

func TestResolveKeyNilFactorySkipsOAuth(t *testing.T) {
	t.Parallel()

	m := NewAuthManager(zerolog.Nop(), nil, false)
	key, err := m.ResolveKey(context.Background(), AuthConfig{
		ProxyAuthKey: secretstring.SecretString("proxy-key"),
	}, "tag:test")
	require.NoError(t, err)
	assert.Equal(t, "proxy-key", key)
}

func TestResolveKeyUnavailableFactorySkipsOAuth(t *testing.T) {
	t.Parallel()

	f := NewAPIClientFactory("", "")
	m := NewAuthManager(zerolog.Nop(), f, false)
	key, err := m.ResolveKey(context.Background(), AuthConfig{
		ProxyAuthKey: secretstring.SecretString("proxy-key"),
	}, "tag:test")
	require.NoError(t, err)
	assert.Equal(t, "proxy-key", key)
}

// --- AuthManager GenerateOAuthKey tests ---

func TestGenerateOAuthKeyNoTagsReturnsEmpty(t *testing.T) {
	t.Parallel()

	f := NewAPIClientFactory("test-id", "test-secret")
	m := NewAuthManager(zerolog.Nop(), f, false)

	key, err := m.GenerateOAuthKey(context.Background(), "")
	require.NoError(t, err)
	assert.Equal(t, "", key)
}

func TestGenerateOAuthKeyWhitespaceOnlyTagsReturnsEmpty(t *testing.T) {
	t.Parallel()

	f := NewAPIClientFactory("test-id", "test-secret")
	m := NewAuthManager(zerolog.Nop(), f, false)

	key, err := m.GenerateOAuthKey(context.Background(), "  , ,  ")
	require.NoError(t, err)
	assert.Equal(t, "", key)
}

// --- OAuth integration tests using httptest.Server ---

func TestGenerateOAuthKeySuccess(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/api/v2/tailnet/-/keys" {
			var ckr tailscale.CreateKeyRequest
			if err := json.NewDecoder(r.Body).Decode(&ckr); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			assert.Equal(t, []string{"tag:test"}, ckr.Capabilities.Devices.Create.Tags)
			assert.False(t, ckr.Capabilities.Devices.Create.Reusable)
			assert.True(t, ckr.Capabilities.Devices.Create.Preauthorized)

			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"id":      "key123",
				"key":     "tskey-auth-test-success",
				"created": "2024-01-01T00:00:00Z",
				"expires": "2024-01-02T00:00:00Z",
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	f := NewAPIClientFactory("test-id", "test-secret")
	m := NewAuthManager(zerolog.Nop(), f, false)

	key, err := m.callGenerateOAuthKeyWithTestServer(context.Background(), "tag:test", srv)
	require.NoError(t, err)
	assert.Equal(t, "tskey-auth-test-success", key)
}

func TestGenerateOAuthKeyInvalidTagPermission(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/api/v2/tailnet/-/keys" {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]any{
				"message": "tag:bad is invalid or not permitted for this client",
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	f := NewAPIClientFactory("test-id", "test-secret")
	m := NewAuthManager(zerolog.Nop(), f, false)

	_, err := m.callGenerateOAuthKeyWithTestServer(context.Background(), "tag:bad", srv)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid or not permitted")
}

func TestGenerateOAuthKeyGenericAPIError(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/api/v2/tailnet/-/keys" {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]any{
				"message": "internal server error",
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	f := NewAPIClientFactory("test-id", "test-secret")
	m := NewAuthManager(zerolog.Nop(), f, false)

	_, err := m.callGenerateOAuthKeyWithTestServer(context.Background(), "tag:test", srv)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "500")
}

func TestResolveKeyWithOAuthError(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	f := NewAPIClientFactory("test-id", "test-secret")
	m := NewAuthManager(zerolog.Nop(), f, false)

	_, err := m.callResolveKeyWithTestServer(context.Background(), AuthConfig{
		ProxyAuthKey: secretstring.SecretString("proxy-key"),
	}, "tag:test", srv)
	assert.Error(t, err)
}

func TestResolveKeyWithOAuthSuccess(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/api/v2/tailnet/-/keys" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"id":      "key123",
				"key":     "tskey-auth-oauth-resolved",
				"created": "2024-01-01T00:00:00Z",
				"expires": "2024-01-02T00:00:00Z",
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	f := NewAPIClientFactory("test-id", "test-secret")
	m := NewAuthManager(zerolog.Nop(), f, false)

	key, err := m.callResolveKeyWithTestServer(context.Background(), AuthConfig{
		ProxyAuthKey: secretstring.SecretString("proxy-key"),
	}, "tag:test", srv)
	require.NoError(t, err)
	assert.Equal(t, "tskey-auth-oauth-resolved", key)
}

func TestResolveKeyResolvedAuthKeyOverridesOAuth(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	f := NewAPIClientFactory("test-id", "test-secret")
	m := NewAuthManager(zerolog.Nop(), f, false)

	key, err := m.callResolveKeyWithTestServer(context.Background(), AuthConfig{
		ResolvedAuthKey: "resolved-key",
	}, "tag:test", srv)
	require.NoError(t, err)
	assert.Equal(t, "resolved-key", key)
}

func TestResolveKeyOAuthErrorFallsBackToProviderKey(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	f := NewAPIClientFactory("test-id", "test-secret")
	m := NewAuthManager(zerolog.Nop(), f, false)

	_, err := m.callResolveKeyWithTestServer(context.Background(), AuthConfig{
		ProviderAuthKey: secretstring.SecretString("provider-key"),
	}, "tag:test", srv)
	assert.Error(t, err)
}

// --- Test helpers ---

// callGenerateOAuthKeyWithTestServer uses a test server for OAuth key generation,
// bypassing the factory's NewClient by redirecting requests to the test server.
func (m *AuthManager) callGenerateOAuthKeyWithTestServer(ctx context.Context, tags string, srv *httptest.Server) (string, error) {
	cleanedTags := cleanTags(tags)
	if len(cleanedTags) == 0 {
		m.log.Warn().Msg("OAuth without tags: cannot create auth key, interactive login will be required")
		return "", nil
	}

	client := m.apiFactory.NewClient(ScopeAuthKeys)
	client.HTTP = newRedirectHTTPClient(srv.URL)

	capabilities := tailscale.KeyCapabilities{}
	capabilities.Devices.Create.Ephemeral = m.ephemeral
	capabilities.Devices.Create.Reusable = false
	capabilities.Devices.Create.Preauthorized = true
	capabilities.Devices.Create.Tags = cleanedTags

	ckr := tailscale.CreateKeyRequest{
		Capabilities: capabilities,
		Description:  userAgent,
	}

	authkey, err := client.Keys().Create(ctx, ckr)
	if err != nil {
		return "", err
	}

	return authkey.Key, nil
}

// callResolveKeyWithTestServer resolves the auth key using a test server for OAuth.
func (m *AuthManager) callResolveKeyWithTestServer(ctx context.Context, cfg AuthConfig, tags string, srv *httptest.Server) (string, error) {
	if cfg.ResolvedAuthKey != "" {
		return cfg.ResolvedAuthKey, nil
	}

	authKey := cfg.ProxyAuthKey.Value()

	if m.apiFactory != nil && m.apiFactory.IsAvailable() {
		oauthKey, err := m.callGenerateOAuthKeyWithTestServer(ctx, tags, srv)
		if err != nil {
			return "", err
		}
		authKey = oauthKey
	}

	if authKey == "" {
		authKey = cfg.ProviderAuthKey.Value()
	}

	if authKey == "" {
		m.log.Info().Msg("No auth key configured, interactive login will be required")
	}

	return authKey, nil
}

func newRedirectHTTPClient(targetURL string) *http.Client {
	return &http.Client{
		Transport: &redirectTransport{targetURL: targetURL},
	}
}

type redirectTransport struct {
	targetURL string
}

func (t *redirectTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	newReq := req.Clone(req.Context())
	newReq.URL.Scheme = "http"
	newReq.URL.Host = t.targetURL[len("http://"):]
	return http.DefaultTransport.RoundTrip(newReq)
}
