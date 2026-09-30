// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package config

import (
	"strings"
	"testing"
)

// newTestConfigForGenerate creates a minimal config for testing generate methods.
func newTestConfigForGenerate(t *testing.T) *Data {
	t.Helper()

	return &Data{
		Docker: make(map[string]*DockerTargetProviderConfig),
		Tailscale: TailscaleProxyProviderConfig{
			Providers: make(map[string]*TailscaleServerConfig),
		},
	}
}

// NOTE: No t.Parallel() in this file — all tests modify global state (env vars).

func TestGenerateDockerConfig_Defaults(t *testing.T) {
	c := newTestConfigForGenerate(t)

	if err := c.generateDockerConfig(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	docker, ok := c.Docker[DockerDefaultName]
	if !ok {
		t.Fatalf("expected Docker config with key %q, not found", DockerDefaultName)
	}

	// Verify the default host from creasty/defaults is set.
	if docker.Host == "" {
		t.Error("expected Docker host to have a default value, got empty string")
	}
}

func TestGenerateDockerConfig_DockerHost(t *testing.T) {
	t.Setenv("DOCKER_HOST", "tcp://custom-host:2375")

	c := newTestConfigForGenerate(t)
	if err := c.generateDockerConfig(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	docker, ok := c.Docker[DockerDefaultName]
	if !ok {
		t.Fatalf("expected Docker config with key %q, not found", DockerDefaultName)
	}

	if docker.Host != "tcp://custom-host:2375" {
		t.Errorf("expected Docker host %q, got %q", "tcp://custom-host:2375", docker.Host)
	}
}

func TestGenerateDockerConfig_TSDProxyHostname(t *testing.T) {
	t.Setenv("TSDPROXY_HOSTNAME", "myhost.example.com")

	c := newTestConfigForGenerate(t)
	if err := c.generateDockerConfig(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	docker, ok := c.Docker[DockerDefaultName]
	if !ok {
		t.Fatalf("expected Docker config with key %q, not found", DockerDefaultName)
	}

	// Note: host.docker.internal DNS lookup may override this in CI environments.
	valid := docker.TargetHostname == "myhost.example.com" ||
		strings.EqualFold(docker.TargetHostname, "host.docker.internal")
	if !valid {
		t.Errorf("TargetHostname = %q, want %q or host.docker.internal", docker.TargetHostname, "myhost.example.com")
	}
}

func TestGenerateTailscaleConfig_Defaults(t *testing.T) {
	c := newTestConfigForGenerate(t)

	if err := c.generateTailscaleConfig(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ts, ok := c.Tailscale.Providers[TailscaleDefaultProviderName]
	if !ok {
		t.Fatalf("expected Tailscale config with key %q, not found", TailscaleDefaultProviderName)
	}

	if ts.ControlURL == "" {
		t.Error("expected ControlURL to have a default value, got empty string")
	}
}

func TestGenerateTailscaleConfig_AuthKeyFile(t *testing.T) {
	t.Setenv("TSDPROXY_AUTHKEYFILE", "/run/secrets/authkey")

	c := newTestConfigForGenerate(t)
	if err := c.generateTailscaleConfig(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ts, ok := c.Tailscale.Providers[TailscaleDefaultProviderName]
	if !ok {
		t.Fatalf("expected Tailscale config with key %q, not found", TailscaleDefaultProviderName)
	}

	if ts.AuthKeyFile != "/run/secrets/authkey" {
		t.Errorf("expected AuthKeyFile %q, got %q", "/run/secrets/authkey", ts.AuthKeyFile)
	}
}

func TestGenerateTailscaleConfig_ControlURL(t *testing.T) {
	t.Setenv("TSDPROXY_CONTROLURL", "https://headscale.example.com")

	c := newTestConfigForGenerate(t)
	if err := c.generateTailscaleConfig(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ts, ok := c.Tailscale.Providers[TailscaleDefaultProviderName]
	if !ok {
		t.Fatalf("expected Tailscale config with key %q, not found", TailscaleDefaultProviderName)
	}

	if ts.ControlURL != "https://headscale.example.com" {
		t.Errorf("expected ControlURL %q, got %q", "https://headscale.example.com", ts.ControlURL)
	}
}

func TestGenerateTailscaleConfig_DataDir(t *testing.T) {
	t.Setenv("TSDPROXY_DATADIR", "/custom/data")

	c := newTestConfigForGenerate(t)
	if err := c.generateTailscaleConfig(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if c.Tailscale.DataDir != "/custom/data" {
		t.Errorf("expected DataDir %q, got %q", "/custom/data", c.Tailscale.DataDir)
	}
}

func TestGenerateTailscaleConfig_SetsDefaultProxyProvider(t *testing.T) {
	c := newTestConfigForGenerate(t)

	if err := c.generateTailscaleConfig(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if c.DefaultProxyProvider != TailscaleDefaultProviderName {
		t.Errorf("expected DefaultProxyProvider %q, got %q", TailscaleDefaultProviderName, c.DefaultProxyProvider)
	}
}

func TestGenerateDefaultProviders(t *testing.T) {
	c := newTestConfigForGenerate(t)

	if err := c.generateDefaultProviders(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, ok := c.Docker[DockerDefaultName]; !ok {
		t.Errorf("expected Docker config with key %q", DockerDefaultName)
	}

	if _, ok := c.Tailscale.Providers[TailscaleDefaultProviderName]; !ok {
		t.Errorf("expected Tailscale config with key %q", TailscaleDefaultProviderName)
	}
}
