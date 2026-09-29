// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package proxmox

import (
	"encoding/base64"
	"testing"
)

func TestParseTsdproxyConfig_Absent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		notes string
	}{
		{"Empty", ""},
		{"PlainText", "just some human notes"},
		{"OtherYAML", "foo: bar\n"},
		{"ScalarBlock", "tsdproxy: hello"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cfg, err := parseTsdproxyConfig(tc.notes)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if cfg == nil {
				t.Fatal("config must be non-nil")
			}
			if cfg.Enable {
				t.Error("expected Enable=false when no tsdproxy data is present")
			}
		})
	}
}

func TestParseTsdproxyConfig_FlatNested(t *testing.T) {
	t.Parallel()

	cfg, err := parseTsdproxyConfig(`
tsdproxy:
  port:
    443: 8443/https:8080/http
  dash.icon: jellyfin
  ratelimit.enabled: true
`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected config")
	}
	if !cfg.Enable {
		t.Error("presence of the block must enable the guest")
	}

	want := map[string]string{
		"tsdproxy.port.443":          "8443/https:8080/http",
		"tsdproxy.dash.icon":         "jellyfin",
		"tsdproxy.ratelimit.enabled": "true",
	}
	for key, value := range want {
		if got := cfg.Settings[key]; got != value {
			t.Errorf("settings[%q]: got %q, want %q", key, got, value)
		}
	}
}

func TestParseTsdproxyConfig_DottedKeys(t *testing.T) {
	t.Parallel()

	cfg, err := parseTsdproxyConfig(`
tsdproxy.enable: true
tsdproxy.port.443: 443/https:8080/http
tsdproxy.name: myvm
`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg == nil || !cfg.Enable {
		t.Fatal("expected enabled config")
	}
	if got := cfg.Settings["tsdproxy.name"]; got != "myvm" {
		t.Errorf("name: got %q, want myvm", got)
	}
}

func TestParseTsdproxyConfig_DottedNestedMap(t *testing.T) {
	t.Parallel()

	cfg, err := parseTsdproxyConfig(`
tsdproxy.port:
  443: 443/https:8080/http
`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := cfg.Settings["tsdproxy.port.443"]; got != "443/https:8080/http" {
		t.Errorf("port: got %q", got)
	}
}

func TestParseTsdproxyConfig_ListStyle(t *testing.T) {
	t.Parallel()

	cfg, err := parseTsdproxyConfig(`
tsdproxy:
  hostname: myvm
  proxyProvider: shared
  dnsProvider: cloudflare
  tlsProvider: acme
  identityHeaders: false
  dashboard:
    label: My VM
    icon: jellyfin
    category: media
    visible: false
  tailscale:
    tags: tag:prod
    ephemeral: true
    runWebClient: true
  ports:
    "443/https":
      targets:
        - "http://10.0.0.5:8080"
      tlsValidate: false
      isRedirect: false
      tailscale:
        funnel: true
`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg == nil || !cfg.Enable {
		t.Fatal("list-style entries must be enabled by presence")
	}

	want := map[string]string{
		"tsdproxy.name":             "myvm",
		"tsdproxy.proxyprovider":    "shared",
		"tsdproxy.dnsprovider":      "cloudflare",
		"tsdproxy.tlsprovider":      "acme",
		"tsdproxy.identity_headers": "false",
		"tsdproxy.dash.label":       "My VM",
		"tsdproxy.dash.icon":        "jellyfin",
		"tsdproxy.dash.category":    "media",
		"tsdproxy.dash.visible":     "false",
		"tsdproxy.tags":             "tag:prod",
		"tsdproxy.ephemeral":        "true",
		"tsdproxy.runwebclient":     "true",
	}
	for key, value := range want {
		if got := cfg.Settings[key]; got != value {
			t.Errorf("settings[%q]: got %q, want %q", key, got, value)
		}
	}

	entry, ok := cfg.Ports["443/https"]
	if !ok {
		t.Fatal("expected list-style port entry")
	}
	if len(entry.Targets) != 1 || entry.Targets[0] != "http://10.0.0.5:8080" {
		t.Errorf("targets: got %+v", entry.Targets)
	}
	if entry.TLSValidate {
		t.Error("tlsValidate: got true, want false")
	}
	if !entry.Tailscale.Funnel {
		t.Error("funnel: got false, want true")
	}
}

func TestParseTsdproxyConfig_FlatKeysWin(t *testing.T) {
	t.Parallel()

	cfg, err := parseTsdproxyConfig(`
tsdproxy:
  name: flat-name
  hostname: list-name
  dash.icon: flat-icon
`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := cfg.Settings["tsdproxy.name"]; got != "flat-name" {
		t.Errorf("name: got %q, want flat-name (flat key wins)", got)
	}
}

func TestParseTsdproxyConfig_MixedStyles(t *testing.T) {
	t.Parallel()

	cfg, err := parseTsdproxyConfig(`
tsdproxy:
  port:
    80: 8080/http:80/http
  ports:
    "443/https":
      targets:
        - "https://example.internal"
`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := cfg.Settings["tsdproxy.port.80"]; got != "8080/http:80/http" {
		t.Errorf("flat port: got %q", got)
	}
	if _, ok := cfg.Ports["443/https"]; !ok {
		t.Error("expected list-style port entry alongside flat port")
	}
}

func TestParseTsdproxyConfig_EnableFalse(t *testing.T) {
	t.Parallel()

	cfg, err := parseTsdproxyConfig("tsdproxy:\n  enable: false\n  port:\n    443: 443/https\n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Enable {
		t.Error("enable: false must disable the guest")
	}
}

func TestParseTsdproxyConfig_Base64(t *testing.T) {
	t.Parallel()

	notes := base64.StdEncoding.EncodeToString([]byte("tsdproxy:\n  name: decoded\n"))
	cfg, err := parseTsdproxyConfig(notes)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected base64 notes to decode")
	}
	if got := cfg.Settings["tsdproxy.name"]; got != "decoded" {
		t.Errorf("name: got %q, want decoded", got)
	}
}

func TestParseTsdproxyConfig_MalformedPorts(t *testing.T) {
	t.Parallel()

	cfg, err := parseTsdproxyConfig("tsdproxy:\n  ports: not-a-map\n")
	if err == nil {
		t.Fatalf("expected error, got cfg %+v", cfg)
	}
}

func TestParseTsdproxyConfig_NullPortEntryKeepsDefaults(t *testing.T) {
	t.Parallel()

	cfg, err := parseTsdproxyConfig("tsdproxy:\n  ports:\n    \"443/https\":\n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	entry, ok := cfg.Ports["443/https"]
	if !ok {
		t.Fatal("expected null port entry to be present")
	}
	if !entry.TLSValidate {
		t.Error("null entry must keep the tlsValidate default")
	}
	if entry.IsRedirect {
		t.Error("null entry must keep the isRedirect default")
	}
}

func TestParseTsdproxyConfig_ScalarNormalization(t *testing.T) {
	t.Parallel()

	cfg, err := parseTsdproxyConfig(`
tsdproxy:
  health_check_interval: 15
  ephemeral: true
  name: 123
`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := cfg.Settings["tsdproxy.health_check_interval"]; got != "15" {
		t.Errorf("int: got %q, want 15", got)
	}
	if got := cfg.Settings["tsdproxy.ephemeral"]; got != "true" {
		t.Errorf("bool: got %q, want true", got)
	}
	if got := cfg.Settings["tsdproxy.name"]; got != "123" {
		t.Errorf("numeric string: got %q, want 123", got)
	}
}
