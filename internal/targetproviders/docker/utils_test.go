// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package docker

import (
	"context"
	"testing"

	"github.com/rs/zerolog"
)

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
		assets:                testAssets,
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
		assets:                testAssets,
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
