// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package config

import (
	"os"
	"path/filepath"
	"testing"
)

func saveEnv(t *testing.T, keys ...string) func() {
	t.Helper()
	saved := make(map[string]string, len(keys))
	for _, k := range keys {
		saved[k] = os.Getenv(k)
	}
	return func() {
		for k, v := range saved {
			if v == "" {
				os.Unsetenv(k)
			} else {
				os.Setenv(k, v)
			}
		}
	}
}

func writeFile(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("writeFile(%s): %v", name, err)
	}
	return path
}

func newSecretsConfig() *config {
	return &config{
		Tailscale: TailscaleProxyProviderConfig{
			Providers: map[string]*TailscaleServerConfig{
				"default": {
					ControlURL: "https://controlplane.tailscale.com",
				},
			},
		},
	}
}

func TestGetAuthKeyFromFile(t *testing.T) {
	t.Parallel()

	c := newSecretsConfig()
	path := writeFile(t, "authkey", "tskey-auth-abcdef123\n")

	key, err := c.getAuthKeyFromFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if key != "tskey-auth-abcdef123" {
		t.Errorf("got %q, want %q", key, "tskey-auth-abcdef123")
	}
}

func TestGetAuthKeyFromFile_TrimsWhitespace(t *testing.T) {
	t.Parallel()

	c := newSecretsConfig()
	path := writeFile(t, "authkey", "  tskey-auth-xyz\n\t  ")

	key, err := c.getAuthKeyFromFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if key != "tskey-auth-xyz" {
		t.Errorf("got %q, want %q", key, "tskey-auth-xyz")
	}
}

func TestGetAuthKeyFromFile_MissingFile(t *testing.T) {
	t.Parallel()

	c := newSecretsConfig()
	_, err := c.getAuthKeyFromFile("/nonexistent/path")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestGetAuthKeyFromFile_Directory(t *testing.T) {
	t.Parallel()

	c := newSecretsConfig()
	_, err := c.getAuthKeyFromFile(t.TempDir())
	if err == nil {
		t.Fatal("expected error for directory path")
	}
}

func TestValidateKeyFilePath_Valid(t *testing.T) {
	t.Parallel()

	path := writeFile(t, "secret.txt", "supersecret")
	resolved, err := ValidateKeyFilePath(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resolved == "" {
		t.Fatal("expected non-empty resolved path")
	}
}

func TestValidateKeyFilePath_NotFound(t *testing.T) {
	t.Parallel()

	_, err := ValidateKeyFilePath("/nonexistent/path/secret.txt")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestValidateKeyFilePath_Directory(t *testing.T) {
	t.Parallel()

	_, err := ValidateKeyFilePath(t.TempDir())
	if err == nil {
		t.Fatal("expected error for directory")
	}
}

func TestLoadTailscaleAuthKeys_FromFile(t *testing.T) {
	t.Parallel()

	c := newSecretsConfig()
	c.Tailscale.Providers["default"].AuthKeyFile = writeFile(t, "authkey", "tskey-auth-from-file")

	if err := c.loadTailscaleAuthKeys(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := c.Tailscale.Providers["default"].AuthKey.Value(); got != "tskey-auth-from-file" {
		t.Errorf("AuthKey = %q, want %q", got, "tskey-auth-from-file")
	}
}

func TestLoadTailscaleAuthKeys_SkipWhenOAuthSet(t *testing.T) {
	t.Parallel()

	c := newSecretsConfig()
	c.Tailscale.Providers["default"].ClientID = "client-id"
	c.Tailscale.Providers["default"].ClientSecret = "client-secret"
	c.Tailscale.Providers["default"].AuthKeyFile = "/nonexistent"

	if err := c.loadTailscaleAuthKeys(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadTailscaleAuthKeys_NoAuthKeyFile(t *testing.T) {
	t.Parallel()

	c := newSecretsConfig()
	if err := c.loadTailscaleAuthKeys(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadTailscaleAuthKeys_MissingAuthKeyFile(t *testing.T) {
	t.Parallel()

	c := newSecretsConfig()
	c.Tailscale.Providers["default"].AuthKeyFile = "/nonexistent/authkey"

	if err := c.loadTailscaleAuthKeys(); err == nil {
		t.Fatal("expected error for missing auth key file")
	}
}

func TestLoadAPIKey_FromFile(t *testing.T) {
	t.Parallel()

	c := newSecretsConfig()
	c.APIKeyFile = writeFile(t, "apikey", "tsdproxy-api-key\n")

	if err := c.loadAPIKey(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.APIKey.Value() != "tsdproxy-api-key" {
		t.Errorf("APIKey = %q, want %q", c.APIKey.Value(), "tsdproxy-api-key")
	}
}

func TestLoadAPIKey_NoFileConfigured(t *testing.T) {
	t.Parallel()

	c := newSecretsConfig()
	c.APIKeyFile = ""

	if err := c.loadAPIKey(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadAPIKey_EmptyFile(t *testing.T) {
	t.Parallel()

	c := newSecretsConfig()
	c.APIKeyFile = writeFile(t, "apikey", "")

	if err := c.loadAPIKey(); err == nil {
		t.Fatal("expected error for empty API key file")
	}
}

func TestLoadAPIKey_OnlyWhitespace(t *testing.T) {
	t.Parallel()

	c := newSecretsConfig()
	c.APIKeyFile = writeFile(t, "apikey", "  \n\t  ")

	if err := c.loadAPIKey(); err == nil {
		t.Fatal("expected error for whitespace-only API key file")
	}
}

func TestLoadAPIKey_MissingFile(t *testing.T) {
	t.Parallel()

	c := newSecretsConfig()
	c.APIKeyFile = "/nonexistent/apikey"

	if err := c.loadAPIKey(); err == nil {
		t.Fatal("expected error for missing API key file")
	}
}

func TestLoadDNSProviderTokens_FromFile(t *testing.T) {
	t.Parallel()

	c := newSecretsConfig()
	c.DNSProviders = map[string]*DNSProviderConfig{
		"cf": {
			Provider:     "cloudflare",
			APITokenFile: writeFile(t, "cf-token", "cf-api-token\n"),
		},
	}

	if err := c.loadDNSProviderTokens(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.DNSProviders["cf"].APIToken.Value() != "cf-api-token" {
		t.Errorf("APIToken = %q, want %q", c.DNSProviders["cf"].APIToken.Value(), "cf-api-token")
	}
}

func TestLoadDNSProviderTokens_NoTokenFile(t *testing.T) {
	t.Parallel()

	c := newSecretsConfig()
	c.DNSProviders = map[string]*DNSProviderConfig{
		"cf": {Provider: "cloudflare", APIToken: "inline-token"},
	}

	if err := c.loadDNSProviderTokens(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.DNSProviders["cf"].APIToken.Value() != "inline-token" {
		t.Errorf("APIToken = %q, want %q", c.DNSProviders["cf"].APIToken.Value(), "inline-token")
	}
}

func TestLoadDNSProviderTokens_MissingFile(t *testing.T) {
	t.Parallel()

	c := newSecretsConfig()
	c.DNSProviders = map[string]*DNSProviderConfig{
		"cf": {
			Provider:     "cloudflare",
			APITokenFile: "/nonexistent/token",
		},
	}

	if err := c.loadDNSProviderTokens(); err == nil {
		t.Fatal("expected error for missing DNS token file")
	}
}

func TestLoadDNSProviderTokens_MultipleProviders(t *testing.T) {
	t.Parallel()

	c := newSecretsConfig()
	c.DNSProviders = map[string]*DNSProviderConfig{
		"cf": {
			Provider:     "cloudflare",
			APITokenFile: writeFile(t, "cf-token", "cf-token\n"),
		},
		"mdns": {
			Provider: "magicdns",
		},
	}

	if err := c.loadDNSProviderTokens(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.DNSProviders["cf"].APIToken.Value() != "cf-token" {
		t.Errorf("cf APIToken = %q, want %q", c.DNSProviders["cf"].APIToken.Value(), "cf-token")
	}
}

func TestLoadTailscaleClientSecrets_FromFile(t *testing.T) {
	t.Parallel()

	c := newSecretsConfig()
	c.Tailscale.Providers["default"].ClientSecretFile = writeFile(t, "client-secret", "ts-client-secret\n")

	if err := c.loadTailscaleClientSecrets(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.Tailscale.Providers["default"].ClientSecret.Value() != "ts-client-secret" {
		t.Errorf("ClientSecret = %q, want %q", c.Tailscale.Providers["default"].ClientSecret.Value(), "ts-client-secret")
	}
}

func TestLoadTailscaleClientSecrets_NoFile(t *testing.T) {
	t.Parallel()

	c := newSecretsConfig()
	if err := c.loadTailscaleClientSecrets(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadTailscaleClientSecrets_EmptyFile(t *testing.T) {
	t.Parallel()

	c := newSecretsConfig()
	c.Tailscale.Providers["default"].ClientSecretFile = writeFile(t, "secret", "")

	if err := c.loadTailscaleClientSecrets(); err == nil {
		t.Fatal("expected error for empty client secret file")
	}
}

func TestLoadTailscaleClientSecrets_MissingFile(t *testing.T) {
	t.Parallel()

	c := newSecretsConfig()
	c.Tailscale.Providers["default"].ClientSecretFile = "/nonexistent/secret"

	if err := c.loadTailscaleClientSecrets(); err == nil {
		t.Fatal("expected error for missing client secret file")
	}
}

func TestLoadTailscaleClientSecrets_MultipleProviders(t *testing.T) {
	t.Parallel()

	c := newSecretsConfig()
	c.Tailscale.Providers["alpha"] = &TailscaleServerConfig{
		ControlURL:       "https://controlplane.tailscale.com",
		ClientSecretFile: writeFile(t, "alpha-secret", "alpha-secret\n"),
	}
	c.Tailscale.Providers["beta"] = &TailscaleServerConfig{
		ControlURL: "https://controlplane.tailscale.com",
	}

	if err := c.loadTailscaleClientSecrets(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.Tailscale.Providers["alpha"].ClientSecret.Value() != "alpha-secret" {
		t.Errorf("alpha ClientSecret = %q, want %q", c.Tailscale.Providers["alpha"].ClientSecret.Value(), "alpha-secret")
	}
	if c.Tailscale.Providers["beta"].ClientSecret.Value() != "" {
		t.Errorf("beta ClientSecret should be empty, got %q", c.Tailscale.Providers["beta"].ClientSecret.Value())
	}
}

func TestLoadSecretsFromFiles_All(t *testing.T) {
	t.Parallel()

	c := newSecretsConfig()
	c.APIKeyFile = writeFile(t, "apikey", "api-key-val\n")
	c.Tailscale.Providers["default"].AuthKeyFile = writeFile(t, "authkey", "tskey-auth-all\n")
	c.DNSProviders = map[string]*DNSProviderConfig{
		"cf": {
			Provider:     "cloudflare",
			APITokenFile: writeFile(t, "cf-token", "cf-token-val\n"),
		},
	}

	if err := c.loadSecretsFromFiles(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.APIKey.Value() != "api-key-val" {
		t.Errorf("APIKey = %q, want %q", c.APIKey.Value(), "api-key-val")
	}
	if c.Tailscale.Providers["default"].AuthKey.Value() != "tskey-auth-all" {
		t.Errorf("AuthKey = %q, want %q", c.Tailscale.Providers["default"].AuthKey.Value(), "tskey-auth-all")
	}
	if c.DNSProviders["cf"].APIToken.Value() != "cf-token-val" {
		t.Errorf("APIToken = %q, want %q", c.DNSProviders["cf"].APIToken.Value(), "cf-token-val")
	}
}

func TestLoadSecretsFromFiles_EmptyAPIKeyReturnsError(t *testing.T) {
	t.Parallel()

	c := newSecretsConfig()
	c.APIKeyFile = writeFile(t, "apikey", "")

	if err := c.loadSecretsFromFiles(); err == nil {
		t.Fatal("expected error for empty API key file")
	}
}

func TestClearSecrets_WipesAPIKeyAndDNSTokens(t *testing.T) {
	t.Parallel()

	c := newSecretsConfig()
	c.APIKey = "should-be-wiped"
	c.DNSProviders = map[string]*DNSProviderConfig{
		"cf": {Provider: "cloudflare", APIToken: "should-be-wiped"},
	}
	c.Tailscale.Providers["default"].AuthKey = "preserved-authkey"
	c.Tailscale.Providers["default"].ClientSecret = "preserved-secret"
	c.Tailscale.Providers["default"].ClientID = "preserved-id"

	c.ClearSecrets()

	if c.APIKey.Value() != "" {
		t.Errorf("APIKey should be empty after ClearSecrets, got %q", c.APIKey.Value())
	}
	if c.DNSProviders["cf"].APIToken.Value() != "" {
		t.Errorf("APIToken should be empty after ClearSecrets, got %q", c.DNSProviders["cf"].APIToken.Value())
	}
	if c.Tailscale.Providers["default"].AuthKey.Value() != "preserved-authkey" {
		t.Errorf("AuthKey should survive ClearSecrets, got %q", c.Tailscale.Providers["default"].AuthKey.Value())
	}
	if c.Tailscale.Providers["default"].ClientSecret.Value() != "preserved-secret" {
		t.Errorf("ClientSecret should survive ClearSecrets, got %q", c.Tailscale.Providers["default"].ClientSecret.Value())
	}
}

func TestLoadTailscaleAuthKeyEnvOverrides(t *testing.T) {
	defer saveEnv(t, "TSDPROXY_AUTHKEY")()

	t.Setenv("TSDPROXY_AUTHKEY", "tskey-auth-env")

	c := newSecretsConfig()
	c.loadTailscaleAuthKeyEnvOverrides()

	if c.Tailscale.Providers["default"].AuthKey.Value() != "tskey-auth-env" {
		t.Errorf("AuthKey = %q, want %q", c.Tailscale.Providers["default"].AuthKey.Value(), "tskey-auth-env")
	}
}

func TestLoadTailscaleAuthKeyEnvOverrides_NotSet(t *testing.T) {
	defer saveEnv(t, "TSDPROXY_AUTHKEY")()

	os.Unsetenv("TSDPROXY_AUTHKEY")

	c := newSecretsConfig()
	c.Tailscale.Providers["default"].AuthKey = "preexisting-key"
	c.loadTailscaleAuthKeyEnvOverrides()

	if c.Tailscale.Providers["default"].AuthKey.Value() != "preexisting-key" {
		t.Errorf("AuthKey should not be overwritten when env var is not set, got %q", c.Tailscale.Providers["default"].AuthKey.Value())
	}
}

func TestLoadTailscaleAuthKeyEnvOverrides_SkipIfAuthKeySet(t *testing.T) {
	defer saveEnv(t, "TSDPROXY_AUTHKEY")()

	t.Setenv("TSDPROXY_AUTHKEY", "tskey-auth-env")

	c := newSecretsConfig()
	c.Tailscale.Providers["default"].AuthKey = "preexisting-key"
	c.loadTailscaleAuthKeyEnvOverrides()

	if c.Tailscale.Providers["default"].AuthKey.Value() != "preexisting-key" {
		t.Errorf("AuthKey should not be overwritten when already set, got %q", c.Tailscale.Providers["default"].AuthKey.Value())
	}
}

func TestLoadTailscaleAuthKeyEnvOverrides_MultipleProviders(t *testing.T) {
	defer saveEnv(t, "TSDPROXY_AUTHKEY")()

	t.Setenv("TSDPROXY_AUTHKEY", "tskey-auth-env")

	c := newSecretsConfig()
	c.Tailscale.Providers["alpha"] = &TailscaleServerConfig{
		ControlURL: "https://controlplane.tailscale.com",
	}
	c.Tailscale.Providers["beta"] = &TailscaleServerConfig{
		ControlURL: "https://controlplane.tailscale.com",
		AuthKey:    "preexisting-beta",
	}
	c.loadTailscaleAuthKeyEnvOverrides()

	if c.Tailscale.Providers["default"].AuthKey.Value() != "tskey-auth-env" {
		t.Errorf("default AuthKey = %q, want %q", c.Tailscale.Providers["default"].AuthKey.Value(), "tskey-auth-env")
	}
	if c.Tailscale.Providers["alpha"].AuthKey.Value() != "tskey-auth-env" {
		t.Errorf("alpha AuthKey = %q, want %q", c.Tailscale.Providers["alpha"].AuthKey.Value(), "tskey-auth-env")
	}
	if c.Tailscale.Providers["beta"].AuthKey.Value() != "preexisting-beta" {
		t.Errorf("beta AuthKey should not be overwritten, got %q", c.Tailscale.Providers["beta"].AuthKey.Value())
	}
}

func TestLoadTailscaleEnvOverrides_Both(t *testing.T) {
	defer saveEnv(t, "TSDPROXY_TAILSCALE_DEFAULT_CLIENTID", "TSDPROXY_TAILSCALE_DEFAULT_CLIENTSECRET")()

	t.Setenv("TSDPROXY_TAILSCALE_DEFAULT_CLIENTID", "env-client-id")
	t.Setenv("TSDPROXY_TAILSCALE_DEFAULT_CLIENTSECRET", "env-client-secret")

	c := newSecretsConfig()
	c.LoadTailscaleEnvOverrides()

	if c.Tailscale.Providers["default"].ClientID != "env-client-id" {
		t.Errorf("ClientID = %q, want %q", c.Tailscale.Providers["default"].ClientID, "env-client-id")
	}
	if c.Tailscale.Providers["default"].ClientSecret.Value() != "env-client-secret" {
		t.Errorf("ClientSecret = %q, want %q", c.Tailscale.Providers["default"].ClientSecret.Value(), "env-client-secret")
	}
}

func TestLoadTailscaleEnvOverrides_OnlyClientID(t *testing.T) {
	defer saveEnv(t, "TSDPROXY_TAILSCALE_DEFAULT_CLIENTID", "TSDPROXY_TAILSCALE_DEFAULT_CLIENTSECRET")()

	t.Setenv("TSDPROXY_TAILSCALE_DEFAULT_CLIENTID", "env-client-id")
	os.Unsetenv("TSDPROXY_TAILSCALE_DEFAULT_CLIENTSECRET")

	c := newSecretsConfig()
	c.LoadTailscaleEnvOverrides()

	if c.Tailscale.Providers["default"].ClientID != "env-client-id" {
		t.Errorf("ClientID = %q, want %q", c.Tailscale.Providers["default"].ClientID, "env-client-id")
	}
	if c.Tailscale.Providers["default"].ClientSecret.Value() != "" {
		t.Errorf("ClientSecret should be empty, got %q", c.Tailscale.Providers["default"].ClientSecret.Value())
	}
}

func TestLoadTailscaleEnvOverrides_SkipWhenAlreadySet(t *testing.T) {
	defer saveEnv(t, "TSDPROXY_TAILSCALE_DEFAULT_CLIENTID")()

	t.Setenv("TSDPROXY_TAILSCALE_DEFAULT_CLIENTID", "env-client-id")

	c := newSecretsConfig()
	c.Tailscale.Providers["default"].ClientID = "preexisting-id"
	c.LoadTailscaleEnvOverrides()

	if c.Tailscale.Providers["default"].ClientID != "preexisting-id" {
		t.Errorf("ClientID should not be overwritten, got %q", c.Tailscale.Providers["default"].ClientID)
	}
}

func TestLoadTailscaleEnvOverrides_ProviderNameWithHyphens(t *testing.T) {
	defer saveEnv(t, "TSDPROXY_TAILSCALE_MY_EU_PROD_CLIENTID")()

	t.Setenv("TSDPROXY_TAILSCALE_MY_EU_PROD_CLIENTID", "env-client-id")

	c := newSecretsConfig()
	c.Tailscale.Providers["my-eu-prod"] = &TailscaleServerConfig{
		ControlURL: "https://controlplane.tailscale.com",
	}
	c.LoadTailscaleEnvOverrides()

	if c.Tailscale.Providers["my-eu-prod"].ClientID != "env-client-id" {
		t.Errorf("ClientID = %q, want %q", c.Tailscale.Providers["my-eu-prod"].ClientID, "env-client-id")
	}
}

func TestLoadTailscaleEnvOverrides_NoProviders(_ *testing.T) {
	c := newSecretsConfig()
	c.Tailscale.Providers = make(map[string]*TailscaleServerConfig)
	c.LoadTailscaleEnvOverrides()
}

func TestIsRunningInDocker(t *testing.T) {
	t.Parallel()
	result := isRunningInDocker()
	if _, err := os.Stat("/.dockerenv"); err == nil && !result {
		t.Error("isRunningInDocker() = false but /.dockerenv exists")
	}
}
