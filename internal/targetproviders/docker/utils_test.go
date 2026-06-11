// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package docker

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/rs/zerolog"

	"github.com/almeidapaulopt/tsdproxy/internal/model"
)

func TestGetLabelBool(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		labels       map[string]string
		label        string
		defaultValue bool
		expected     bool
	}{
		{name: "true label", labels: map[string]string{"enable": "true"}, label: "enable", defaultValue: false, expected: true},
		{name: "false label", labels: map[string]string{"enable": "false"}, label: "enable", defaultValue: true, expected: false},
		{name: "1 as true", labels: map[string]string{"enable": "1"}, label: "enable", defaultValue: false, expected: true},
		{name: "0 as false", labels: map[string]string{"enable": "0"}, label: "enable", defaultValue: true, expected: false},
		{name: "invalid value uses default", labels: map[string]string{"enable": "notabool"}, label: "enable", defaultValue: true, expected: true},
		{name: "missing label uses default (false)", labels: map[string]string{}, label: "enable", defaultValue: false, expected: false},
		{name: "missing label uses default (true)", labels: map[string]string{}, label: "enable", defaultValue: true, expected: true},
		{name: "TRUE case insensitive", labels: map[string]string{"enable": "TRUE"}, label: "enable", defaultValue: false, expected: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c := &container{log: zerolog.Nop(), labels: tt.labels}
			result := c.getLabelBool(tt.label, tt.defaultValue)
			if result != tt.expected {
				t.Errorf("getLabelBool(%q, %v) = %v, want %v", tt.label, tt.defaultValue, result, tt.expected)
			}
		})
	}
}

func TestGetLabelString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		labels       map[string]string
		label        string
		defaultValue string
		expected     string
	}{
		{name: "label exists", labels: map[string]string{"name": "my-service"}, label: "name", defaultValue: "default", expected: "my-service"},
		{name: "label missing", labels: map[string]string{}, label: "name", defaultValue: "default", expected: "default"},
		{name: "empty string label", labels: map[string]string{"name": ""}, label: "name", defaultValue: "default", expected: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c := &container{log: zerolog.Nop(), labels: tt.labels}
			result := c.getLabelString(tt.label, tt.defaultValue)
			if result != tt.expected {
				t.Errorf("getLabelString(%q, %q) = %q, want %q", tt.label, tt.defaultValue, result, tt.expected)
			}
		})
	}
}

func TestGetLabelInt(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		labels       map[string]string
		label        string
		defaultValue int
		min          int
		max          int
		expected     int
	}{
		{name: "valid in range", labels: map[string]string{"interval": "30"}, label: "interval", defaultValue: 10, min: 1, max: 100, expected: 30},
		{name: "below min returns default", labels: map[string]string{"interval": "0"}, label: "interval", defaultValue: 10, min: 1, max: 100, expected: 10},
		{name: "above max returns default", labels: map[string]string{"interval": "200"}, label: "interval", defaultValue: 10, min: 1, max: 100, expected: 10},
		{name: "not a number returns default", labels: map[string]string{"interval": "abc"}, label: "interval", defaultValue: 10, min: 1, max: 100, expected: 10},
		{name: "missing label returns default", labels: map[string]string{}, label: "interval", defaultValue: 10, min: 1, max: 100, expected: 10},
		{name: "value equals min", labels: map[string]string{"interval": "1"}, label: "interval", defaultValue: 10, min: 1, max: 100, expected: 1},
		{name: "value equals max", labels: map[string]string{"interval": "100"}, label: "interval", defaultValue: 10, min: 1, max: 100, expected: 100},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c := &container{log: zerolog.Nop(), labels: tt.labels}
			result := c.getLabelInt(tt.label, tt.defaultValue, tt.min, tt.max)
			if result != tt.expected {
				t.Errorf("getLabelInt(%q, %d, %d, %d) = %d, want %d", tt.label, tt.defaultValue, tt.min, tt.max, result, tt.expected)
			}
		})
	}
}

func TestGetAuthKeyFromAuthFile(t *testing.T) {
	t.Parallel()

	t.Run("no authkeyfile label", func(t *testing.T) {
		t.Parallel()
		c := &container{log: zerolog.Nop(), labels: map[string]string{}}
		result, err := c.getAuthKeyFromAuthFile("tskey-static-key")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.Value() != "tskey-static-key" {
			t.Errorf("got %q, want %q", result.Value(), "tskey-static-key")
		}
	})

	t.Run("empty authkeyfile label", func(t *testing.T) {
		t.Parallel()
		c := &container{log: zerolog.Nop(), labels: map[string]string{LabelAuthKeyFile: ""}}
		result, err := c.getAuthKeyFromAuthFile("tskey-key")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.Value() != "tskey-key" {
			t.Errorf("got %q, want %q", result.Value(), "tskey-key")
		}
	})

	t.Run("read from valid file", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		keyPath := filepath.Join(dir, "authkey.txt")
		if err := os.WriteFile(keyPath, []byte("tskey-from-file\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		c := &container{log: zerolog.Nop(), labels: map[string]string{LabelAuthKeyFile: keyPath}}
		result, err := c.getAuthKeyFromAuthFile("")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.Value() != "tskey-from-file" {
			t.Errorf("got %q, want %q", result.Value(), "tskey-from-file")
		}
	})

	t.Run("nonexistent file returns error", func(t *testing.T) {
		t.Parallel()
		c := &container{log: zerolog.Nop(), labels: map[string]string{LabelAuthKeyFile: "/nonexistent/path/key.txt"}}
		_, err := c.getAuthKeyFromAuthFile("")
		if err == nil {
			t.Fatal("expected error for nonexistent file")
		}
	})
}

func TestGetProxyHostname(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		labels   map[string]string
		contName string
		want     string
		wantErr  bool
	}{
		{name: "custom valid name", labels: map[string]string{"tsdproxy.name": "my-service"}, contName: "/container", want: "my-service", wantErr: false},
		{name: "falls back to container name", labels: map[string]string{}, contName: "/my-container", want: "my-container", wantErr: false},
		{name: "falls back strips leading slash", labels: map[string]string{}, contName: "/proxy-1", want: "proxy-1", wantErr: false},
		{name: "invalid uppercase", labels: map[string]string{"tsdproxy.name": "My_Service"}, contName: "/c", want: "", wantErr: true},
		{name: "invalid with underscore", labels: map[string]string{"tsdproxy.name": "bad_name"}, contName: "/c", want: "", wantErr: true},
		{name: "invalid starts with hyphen", labels: map[string]string{"tsdproxy.name": "-bad"}, contName: "/c", want: "", wantErr: true},
		{name: "valid alphanumeric", labels: map[string]string{"tsdproxy.name": "myservice123"}, contName: "/c", want: "myservice123", wantErr: false},
		{name: "valid with hyphens", labels: map[string]string{"tsdproxy.name": "my-proxy-1"}, contName: "/c", want: "my-proxy-1", wantErr: false},
		{name: "single char valid", labels: map[string]string{"tsdproxy.name": "a"}, contName: "/c", want: "a", wantErr: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c := &container{log: zerolog.Nop(), labels: tt.labels, name: tt.contName}
			result, err := c.getProxyHostname()
			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error, got %q", result)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result != tt.want {
				t.Errorf("getProxyHostname() = %q, want %q", result, tt.want)
			}
		})
	}
}

func TestApplyPortOptions(t *testing.T) {
	t.Parallel()

	c := &container{log: zerolog.Nop()}

	t.Run("empty options", func(t *testing.T) {
		t.Parallel()
		port := model.PortConfig{TLSValidate: true, Tailscale: model.TailscalePort{}}
		c.applyPortOptions("port.test", &port, nil)
		if !port.TLSValidate {
			t.Error("expected TLSValidate=true by default")
		}
		if port.Tailscale.Funnel {
			t.Error("expected Funnel=false by default")
		}
		if port.NoAutoDetect {
			t.Error("expected NoAutoDetect=false by default")
		}
	})

	t.Run("no_tlsvalidate option", func(t *testing.T) {
		t.Parallel()
		port := model.PortConfig{TLSValidate: true}
		c.applyPortOptions("port.test", &port, []string{"no_tlsvalidate"})
		if port.TLSValidate {
			t.Error("expected TLSValidate=false")
		}
	})

	t.Run("tailscale_funnel option", func(t *testing.T) {
		t.Parallel()
		port := model.PortConfig{TLSValidate: true}
		c.applyPortOptions("port.test", &port, []string{"tailscale_funnel"})
		if !port.Tailscale.Funnel {
			t.Error("expected Funnel=true")
		}
	})

	t.Run("no_autodetect option", func(t *testing.T) {
		t.Parallel()
		port := model.PortConfig{TLSValidate: true}
		c.applyPortOptions("port.test", &port, []string{"no_autodetect"})
		if !port.NoAutoDetect {
			t.Error("expected NoAutoDetect=true")
		}
	})

	t.Run("multiple options", func(t *testing.T) {
		t.Parallel()
		port := model.PortConfig{TLSValidate: true}
		c.applyPortOptions("port.test", &port, []string{"no_tlsvalidate", "tailscale_funnel"})
		if port.TLSValidate {
			t.Error("expected TLSValidate=false with no_tlsvalidate")
		}
		if !port.Tailscale.Funnel {
			t.Error("expected Funnel=true with tailscale_funnel")
		}
	})

	t.Run("unknown option ignored", func(t *testing.T) {
		t.Parallel()
		port := model.PortConfig{TLSValidate: true}
		c.applyPortOptions("port.test", &port, []string{"unknown_option"})
		if !port.TLSValidate {
			t.Error("expected TLSValidate=true (unknown option ignored)")
		}
	})

	t.Run("whitespace trimming", func(t *testing.T) {
		t.Parallel()
		port := model.PortConfig{TLSValidate: true}
		c.applyPortOptions("port.test", &port, []string{" no_tlsvalidate "})
		if port.TLSValidate {
			t.Error("expected TLSValidate=false after trimming whitespace")
		}
	})
}

func TestNewProxyConfig_Minimal(t *testing.T) {
	c := &container{
		log:                   zerolog.Nop(),
		id:                    "test-id-123",
		name:                  "/my-app",
		image:                 "nginx:alpine",
		targetProviderName:    "default",
		defaultTargetHostname: "host.docker.internal",
		ipAddress:             nil,
		ports:                 map[string]string{"80": "8080"},
		labels:                map[string]string{},
		networkMode:           "bridge",
		autodetect:            false,
		autoRestart:           false,
		healthCheckEnabled:    false,
	}

	pcfg, err := c.newProxyConfig(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if pcfg.Hostname != "my-app" {
		t.Errorf("Hostname = %q, want %q", pcfg.Hostname, "my-app")
	}
	if pcfg.ProxyProvider != "" {
		t.Errorf("ProxyProvider = %q, want %q (empty = use global default)", pcfg.ProxyProvider, "")
	}
	if pcfg.TargetID != "test-id-123" {
		t.Errorf("TargetID = %q, want %q", pcfg.TargetID, "test-id-123")
	}
	if pcfg.TargetImage != "nginx:alpine" {
		t.Errorf("TargetImage = %q, want %q", pcfg.TargetImage, "nginx:alpine")
	}
}

func TestNewProxyConfig_InvalidHostname(t *testing.T) {
	c := &container{
		log:                   zerolog.Nop(),
		id:                    "test-id",
		name:                  "/container",
		labels:                map[string]string{"tsdproxy.name": "INVALID_HOST"},
		targetProviderName:    "local",
		defaultTargetHostname: "host.docker.internal",
		ports:                 map[string]string{"80": "8080"},
		networkMode:           "bridge",
	}

	_, err := c.newProxyConfig(context.Background())
	if err == nil {
		t.Fatal("expected error for invalid hostname")
	}
}

func TestGetTailscaleConfig(t *testing.T) {
	t.Parallel()

	t.Run("all defaults", func(t *testing.T) {
		t.Parallel()
		c := &container{log: zerolog.Nop(), labels: map[string]string{}}
		ts, err := c.getTailscaleConfig()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if ts.Ephemeral {
			t.Error("expected Ephemeral=false")
		}
		if ts.RunWebClient {
			t.Error("expected RunWebClient=false")
		}
		if ts.Verbose {
			t.Error("expected Verbose=false")
		}
		if ts.AuthKey.Value() != "" {
			t.Errorf("expected empty AuthKey, got %q", ts.AuthKey.Value())
		}
		if ts.Tags != "" {
			t.Errorf("expected empty Tags, got %q", ts.Tags)
		}
	})

	t.Run("ephemeral true", func(t *testing.T) {
		t.Parallel()
		c := &container{log: zerolog.Nop(), labels: map[string]string{"tsdproxy.ephemeral": "true"}}
		ts, err := c.getTailscaleConfig()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !ts.Ephemeral {
			t.Error("expected Ephemeral=true")
		}
	})

	t.Run("auth key from label", func(t *testing.T) {
		t.Parallel()
		c := &container{log: zerolog.Nop(), labels: map[string]string{"tsdproxy.authkey": "tskey-test-123"}}
		ts, err := c.getTailscaleConfig()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if ts.AuthKey.Value() != "tskey-test-123" {
			t.Errorf("AuthKey = %q, want %q", ts.AuthKey.Value(), "tskey-test-123")
		}
	})

	t.Run("tags from label", func(t *testing.T) {
		t.Parallel()
		c := &container{log: zerolog.Nop(), labels: map[string]string{"tsdproxy.tags": "tag:web,tag:dev"}}
		ts, err := c.getTailscaleConfig()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if ts.Tags != "tag:web,tag:dev" {
			t.Errorf("Tags = %q, want %q", ts.Tags, "tag:web,tag:dev")
		}
	})

	t.Run("run web client", func(t *testing.T) {
		t.Parallel()
		c := &container{log: zerolog.Nop(), labels: map[string]string{"tsdproxy.runwebclient": "true"}}
		ts, err := c.getTailscaleConfig()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !ts.RunWebClient {
			t.Error("expected RunWebClient=true")
		}
	})
}

func TestGetPorts_NoPorts(t *testing.T) {
	t.Parallel()

	c := &container{
		log:    zerolog.Nop(),
		labels: map[string]string{},
	}

	ports := c.getPorts(context.Background())
	if len(ports) != 0 {
		t.Errorf("expected 0 ports, got %d", len(ports))
	}
}

func TestGetPorts_SinglePort(t *testing.T) {
	t.Parallel()

	c := &container{
		log:                   zerolog.Nop(),
		labels:                map[string]string{"tsdproxy.port.web": "443/https:80/http"},
		defaultTargetHostname: "host.docker.internal",
		ports:                 map[string]string{"80": "8080"},
		networkMode:           "bridge",
		autodetect:            false,
	}

	ports := c.getPorts(context.Background())
	if len(ports) != 1 {
		t.Fatalf("expected 1 port, got %d", len(ports))
	}
}

func TestGetPorts_WithRedirect(t *testing.T) {
	t.Parallel()

	c := &container{
		log:    zerolog.Nop(),
		labels: map[string]string{"tsdproxy.port.1": "81/http->https://example.ts.net"},
	}

	ports := c.getPorts(context.Background())
	if len(ports) != 1 {
		t.Fatalf("expected 1 port, got %d", len(ports))
	}
	for k, p := range ports {
		if !p.IsRedirect {
			t.Errorf("port %q: expected IsRedirect=true", k)
		}
	}
}

func TestGetPorts_InvalidLabel(t *testing.T) {
	t.Parallel()

	c := &container{
		log:    zerolog.Nop(),
		labels: map[string]string{"tsdproxy.port.bad": "::garbage"},
	}

	ports := c.getPorts(context.Background())
	if len(ports) != 0 {
		t.Errorf("expected 0 ports for invalid label, got %d", len(ports))
	}
}
