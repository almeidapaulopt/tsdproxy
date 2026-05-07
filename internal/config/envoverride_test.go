// SPDX-FileCopyrightText: 2025 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package config

import (
	"testing"

	"github.com/creasty/defaults"
)

func newTestConfig(t *testing.T) *config {
	t.Helper()

	cfg := &config{}
	cfg.Tailscale.Providers = make(map[string]*TailscaleServerConfig)
	cfg.Docker = make(map[string]*DockerTargetProviderConfig)
	cfg.Lists = make(map[string]*ListTargetProviderConfig)

	if err := defaults.Set(cfg); err != nil {
		t.Fatalf("defaults.Set: %v", err)
	}

	return cfg
}

func TestApplyEnvOverrides_SimpleString(t *testing.T) {
	cfg := newTestConfig(t)

	t.Setenv("TSDPROXY_DEFAULTPROXYPROVIDER", "myprovider")

	if err := applyEnvOverrides(cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.DefaultProxyProvider != "myprovider" {
		t.Errorf("DefaultProxyProvider = %q, want %q", cfg.DefaultProxyProvider, "myprovider")
	}
}

func TestApplyEnvOverrides_BoolField(t *testing.T) {
	cfg := newTestConfig(t)

	t.Setenv("TSDPROXY_PROXYACCESSLOG", "false")

	if err := applyEnvOverrides(cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.ProxyAccessLog != false {
		t.Errorf("ProxyAccessLog = %v, want false", cfg.ProxyAccessLog)
	}
}

func TestApplyEnvOverrides_Uint16Field(t *testing.T) {
	cfg := newTestConfig(t)

	t.Setenv("TSDPROXY_HTTP_PORT", "9090")

	if err := applyEnvOverrides(cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.HTTP.Port != 9090 {
		t.Errorf("HTTP.Port = %d, want 9090", cfg.HTTP.Port)
	}
}

func TestApplyEnvOverrides_NestedStruct(t *testing.T) {
	cfg := newTestConfig(t)

	t.Setenv("TSDPROXY_LOG_LEVEL", "debug")

	if err := applyEnvOverrides(cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Log.Level != "debug" {
		t.Errorf("Log.Level = %q, want %q", cfg.Log.Level, "debug")
	}
}

func TestApplyEnvOverrides_TailscaleDataDir(t *testing.T) {
	cfg := newTestConfig(t)

	t.Setenv("TSDPROXY_TAILSCALE_DATADIR", "/custom/data")

	if err := applyEnvOverrides(cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Tailscale.DataDir != "/custom/data" {
		t.Errorf("Tailscale.DataDir = %q, want %q", cfg.Tailscale.DataDir, "/custom/data")
	}
}

func TestApplyEnvOverrides_ExistingMapEntry(t *testing.T) {
	cfg := newTestConfig(t)
	cfg.Tailscale.Providers["default"] = &TailscaleServerConfig{}

	t.Setenv("TSDPROXY_TAILSCALE_PROVIDERS_DEFAULT_CLIENTID", "my-client-id")

	if err := applyEnvOverrides(cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	p := cfg.Tailscale.Providers["default"]
	if p == nil {
		t.Fatal("provider 'default' is nil")
	}

	if p.ClientID != "my-client-id" {
		t.Errorf("ClientID = %q, want %q", p.ClientID, "my-client-id")
	}
}

func TestApplyEnvOverrides_AutoCreateMapEntry(t *testing.T) {
	cfg := newTestConfig(t)

	t.Setenv("TSDPROXY_TAILSCALE_PROVIDERS_NEWPROV_CLIENTSECRET", "secret123")

	if err := applyEnvOverrides(cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	p := cfg.Tailscale.Providers["newprov"]
	if p == nil {
		t.Fatal("provider 'newprov' was not auto-created")
	}

	if p.ClientSecret != "secret123" {
		t.Errorf("ClientSecret = %q, want %q", p.ClientSecret, "secret123")
	}

	// Verify defaults were applied to the new entry.
	if p.ControlURL != "https://controlplane.tailscale.com" {
		t.Errorf("ControlURL = %q, want default", p.ControlURL)
	}
}

func TestApplyEnvOverrides_DockerMapEntry(t *testing.T) {
	cfg := newTestConfig(t)

	t.Setenv("TSDPROXY_DOCKER_LOCAL_HOST", "tcp://1.2.3.4:2376")

	if err := applyEnvOverrides(cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	d := cfg.Docker["local"]
	if d == nil {
		t.Fatal("docker 'local' was not auto-created")
	}

	if d.Host != "tcp://1.2.3.4:2376" {
		t.Errorf("Host = %q, want %q", d.Host, "tcp://1.2.3.4:2376")
	}
}

func TestApplyEnvOverrides_MultiWordYAMLTag(t *testing.T) {
	cfg := newTestConfig(t)
	cfg.Docker["local"] = &DockerTargetProviderConfig{}

	t.Setenv("TSDPROXY_DOCKER_LOCAL_TRYDOCKERINTERNALNETWORK", "true")

	if err := applyEnvOverrides(cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Docker["local"].TryDockerInternalNetwork != true {
		t.Error("TryDockerInternalNetwork should be true")
	}
}

func TestApplyEnvOverrides_MultipleEnvVars(t *testing.T) {
	cfg := newTestConfig(t)

	t.Setenv("TSDPROXY_TAILSCALE_PROVIDERS_DEFAULT_CLIENTID", "id1")
	t.Setenv("TSDPROXY_TAILSCALE_PROVIDERS_DEFAULT_CLIENTSECRET", "secret1")
	t.Setenv("TSDPROXY_TAILSCALE_PROVIDERS_DEFAULT_TAGS", "tag:web")
	t.Setenv("TSDPROXY_LOG_LEVEL", "debug")

	if err := applyEnvOverrides(cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	p := cfg.Tailscale.Providers["default"]
	if p == nil {
		t.Fatal("provider 'default' is nil")
	}

	if p.ClientID != "id1" {
		t.Errorf("ClientID = %q, want %q", p.ClientID, "id1")
	}

	if p.ClientSecret != "secret1" {
		t.Errorf("ClientSecret = %q, want %q", p.ClientSecret, "secret1")
	}

	if p.Tags != "tag:web" {
		t.Errorf("Tags = %q, want %q", p.Tags, "tag:web")
	}

	if cfg.Log.Level != "debug" {
		t.Errorf("Log.Level = %q, want %q", cfg.Log.Level, "debug")
	}
}

func TestApplyEnvOverrides_LegacyEnvVarsSkipped(t *testing.T) {
	cfg := newTestConfig(t)

	t.Setenv("TSDPROXY_AUTHKEY", "should-be-ignored")
	t.Setenv("TSDPROXY_AUTHKEYFILE", "should-be-ignored")
	t.Setenv("TSDPROXY_CONTROLURL", "should-be-ignored")
	t.Setenv("TSDPROXY_DATADIR", "should-be-ignored")
	t.Setenv("TSDPROXY_HOSTNAME", "should-be-ignored")

	if err := applyEnvOverrides(cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestApplyEnvOverrides_InvalidBool(t *testing.T) {
	cfg := newTestConfig(t)

	t.Setenv("TSDPROXY_PROXYACCESSLOG", "notabool")

	err := applyEnvOverrides(cfg)
	if err == nil {
		t.Fatal("expected error for invalid bool value")
	}
}

func TestApplyEnvOverrides_InvalidUint16(t *testing.T) {
	cfg := newTestConfig(t)

	t.Setenv("TSDPROXY_HTTP_PORT", "99999")

	err := applyEnvOverrides(cfg)
	if err == nil {
		t.Fatal("expected error for out-of-range uint16 value")
	}
}

func TestApplyEnvOverrides_UnknownPath(t *testing.T) {
	cfg := newTestConfig(t)

	t.Setenv("TSDPROXY_NONEXISTENT_FIELD", "value")

	err := applyEnvOverrides(cfg)
	if err == nil {
		t.Fatal("expected error for unknown path")
	}
}

func TestApplyEnvOverrides_TailscaleProviderControlURL(t *testing.T) {
	cfg := newTestConfig(t)

	t.Setenv("TSDPROXY_TAILSCALE_PROVIDERS_HEADSCALE_CONTROLURL", "http://headscale.local")

	if err := applyEnvOverrides(cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	p := cfg.Tailscale.Providers["headscale"]
	if p == nil {
		t.Fatal("provider 'headscale' was not auto-created")
	}

	if p.ControlURL != "http://headscale.local" {
		t.Errorf("ControlURL = %q, want %q", p.ControlURL, "http://headscale.local")
	}
}

func TestApplyEnvOverrides_HTTPHostname(t *testing.T) {
	cfg := newTestConfig(t)

	t.Setenv("TSDPROXY_HTTP_HOSTNAME", "127.0.0.1")

	if err := applyEnvOverrides(cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.HTTP.Hostname != "127.0.0.1" {
		t.Errorf("HTTP.Hostname = %q, want %q", cfg.HTTP.Hostname, "127.0.0.1")
	}
}

func TestApplyEnvOverrides_LogJSON(t *testing.T) {
	cfg := newTestConfig(t)

	t.Setenv("TSDPROXY_LOG_JSON", "true")

	if err := applyEnvOverrides(cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Log.JSON != true {
		t.Error("Log.JSON should be true")
	}
}

func TestApplyEnvOverrides_NonTSDPROXYIgnored(t *testing.T) {
	cfg := newTestConfig(t)

	t.Setenv("OTHER_VAR", "value")
	t.Setenv("HOME", "/home/user")

	if err := applyEnvOverrides(cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestApplyEnvOverrides_DockerDefaultProxyProvider(t *testing.T) {
	cfg := newTestConfig(t)
	cfg.Docker["remote"] = &DockerTargetProviderConfig{}

	t.Setenv("TSDPROXY_DOCKER_REMOTE_DEFAULTPROXYPROVIDER", "server1")

	if err := applyEnvOverrides(cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Docker["remote"].DefaultProxyProvider != "server1" {
		t.Errorf("DefaultProxyProvider = %q, want %q",
			cfg.Docker["remote"].DefaultProxyProvider, "server1")
	}
}
