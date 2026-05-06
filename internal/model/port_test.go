// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package model

import (
	"net/url"
	"testing"
)

func TestNewPortLongLabel_TCPProxyPort(t *testing.T) {
	cfg, err := NewPortLongLabel("8222/tcp:22/tcp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.ProxyPort != 8222 {
		t.Errorf("ProxyPort: got %d, want 8222", cfg.ProxyPort)
	}
	if cfg.ProxyProtocol != "tcp" {
		t.Errorf("ProxyProtocol: got %q, want \"tcp\"", cfg.ProxyProtocol)
	}

	target := cfg.GetFirstTarget()
	if target.Scheme != "tcp" {
		t.Errorf("target scheme: got %q, want \"tcp\"", target.Scheme)
	}
	if target.Host != "0.0.0.0:22" {
		t.Errorf("target host: got %q, want \"0.0.0.0:22\"", target.Host)
	}
}

func TestNewPortLongLabel_HTTPSProxyWithHTTPTarget(t *testing.T) {
	cfg, err := NewPortLongLabel("443/https:3000/http")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.ProxyPort != 443 {
		t.Errorf("ProxyPort: got %d, want 443", cfg.ProxyPort)
	}
	if cfg.ProxyProtocol != "https" {
		t.Errorf("ProxyProtocol: got %q, want \"https\"", cfg.ProxyProtocol)
	}

	target := cfg.GetFirstTarget()
	if target.Scheme != "http" {
		t.Errorf("target scheme: got %q, want \"http\"", target.Scheme)
	}
	if target.Host != "0.0.0.0:3000" {
		t.Errorf("target host: got %q, want \"0.0.0.0:3000\"", target.Host)
	}
}

func TestNewPortLongLabel_ShortForm_DefaultsToHTTPS_HTTP(t *testing.T) {
	cfg, err := NewPortLongLabel("443:80")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.ProxyPort != 443 {
		t.Errorf("ProxyPort: got %d, want 443", cfg.ProxyPort)
	}
	if cfg.ProxyProtocol != "https" {
		t.Errorf("ProxyProtocol: got %q, want \"https\" (default)", cfg.ProxyProtocol)
	}

	target := cfg.GetFirstTarget()
	if target.Scheme != "http" {
		t.Errorf("target scheme: got %q, want \"http\" (default)", target.Scheme)
	}
	if target.Host != "0.0.0.0:80" {
		t.Errorf("target host: got %q, want \"0.0.0.0:80\"", target.Host)
	}
}

func TestNewPortLongLabel_TCPDifferentPorts(t *testing.T) {
	cfg, err := NewPortLongLabel("2222/tcp:22/tcp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.ProxyPort != 2222 {
		t.Errorf("ProxyPort: got %d, want 2222", cfg.ProxyPort)
	}
	if cfg.ProxyProtocol != "tcp" {
		t.Errorf("ProxyProtocol: got %q, want \"tcp\"", cfg.ProxyProtocol)
	}

	target := cfg.GetFirstTarget()
	if target.Scheme != "tcp" {
		t.Errorf("target scheme: got %q, want \"tcp\"", target.Scheme)
	}
	if target.Host != "0.0.0.0:22" {
		t.Errorf("target host: got %q, want \"0.0.0.0:22\"", target.Host)
	}
}

func TestNewPortLongLabel_TCPDefaultTargetProtocol(t *testing.T) {
	cfg, err := NewPortLongLabel("8222/tcp:22")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	target := cfg.GetFirstTarget()

	// When no target protocol is specified after the port,
	// parseTargetSegment now defaults to the proxy protocol for TCP proxies.
	// This fixes the issue where "8222/tcp:22" would produce an "http" target
	// scheme, causing incorrect routing through Docker's HTTP port mapping.
	if target.Scheme != "tcp" {
		t.Errorf("target scheme: got %q, want \"tcp\" (should match proxy protocol)", target.Scheme)
	}
	if target.Host != "0.0.0.0:22" {
		t.Errorf("target host: got %q, want \"0.0.0.0:22\"", target.Host)
	}
}

func TestNewPortLongLabel_Redirect(t *testing.T) {
	cfg, err := NewPortLongLabel("443/https->https://example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !cfg.IsRedirect {
		t.Error("IsRedirect: got false, want true")
	}
	if cfg.ProxyPort != 443 {
		t.Errorf("ProxyPort: got %d, want 443", cfg.ProxyPort)
	}

	target := cfg.GetFirstTarget()
	if target.Scheme != "https" {
		t.Errorf("target scheme: got %q, want \"https\"", target.Scheme)
	}
	if target.Host != "example.com" {
		t.Errorf("target host: got %q, want \"example.com\"", target.Host)
	}
}

func TestNewPortLongLabel_TargetPortWithoutProtocol(t *testing.T) {
	cfg, err := NewPortLongLabel("443/https:8080")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	target := cfg.GetFirstTarget()
	if target.Scheme != "http" {
		t.Errorf("target scheme: got %q, want \"http\" (default)", target.Scheme)
	}
	if target.Host != "0.0.0.0:8080" {
		t.Errorf("target host: got %q, want \"0.0.0.0:8080\"", target.Host)
	}
}

func TestNewPortLongLabel_InvalidFormat(t *testing.T) {
	_, err := NewPortLongLabel("invalid")
	if err == nil {
		t.Error("expected error for format without separator")
	}
}

func TestNewPortLongLabel_InvalidProxyPort(t *testing.T) {
	_, err := NewPortLongLabel("abc/tcp:22/tcp")
	if err == nil {
		t.Error("expected error for non-numeric proxy port")
	}
}

func TestNewPortLongLabel_InvalidTargetPort(t *testing.T) {
	_, err := NewPortLongLabel("8222/tcp:abc/tcp")
	if err == nil {
		t.Error("expected error for non-numeric target port")
	}
}

func TestNewPortShortLabel_HTTPS(t *testing.T) {
	cfg, err := NewPortShortLabel("443/https")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.ProxyPort != 443 {
		t.Errorf("ProxyPort: got %d, want 443", cfg.ProxyPort)
	}
	if cfg.ProxyProtocol != "https" {
		t.Errorf("ProxyProtocol: got %q, want \"https\"", cfg.ProxyProtocol)
	}
}

func TestNewPortShortLabel_TCP(t *testing.T) {
	cfg, err := NewPortShortLabel("22/tcp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.ProxyPort != 22 {
		t.Errorf("ProxyPort: got %d, want 22", cfg.ProxyPort)
	}
	if cfg.ProxyProtocol != "tcp" {
		t.Errorf("ProxyProtocol: got %q, want \"tcp\"", cfg.ProxyProtocol)
	}
}

func TestNewPortLongLabel_ProxyPortOutOfRange(t *testing.T) {
	_, err := NewPortLongLabel("0/tcp:22/tcp")
	if err == nil {
		t.Error("expected error for proxy port 0")
	}

	_, err = NewPortLongLabel("65536/tcp:22/tcp")
	if err == nil {
		t.Error("expected error for proxy port 65536")
	}

	_, err = NewPortLongLabel("-1/tcp:22/tcp")
	if err == nil {
		t.Error("expected error for negative proxy port")
	}
}

func TestNewPortLongLabel_TargetPortOutOfRange(t *testing.T) {
	_, err := NewPortLongLabel("8222/tcp:0/tcp")
	if err == nil {
		t.Error("expected error for target port 0")
	}

	_, err = NewPortLongLabel("8222/tcp:65536/tcp")
	if err == nil {
		t.Error("expected error for target port 65536")
	}
}

func TestNewPortLongLabel_PortBoundaryValues(t *testing.T) {
	cfg, err := NewPortLongLabel("443/https:1/http")
	if err != nil {
		t.Fatalf("unexpected error for port 1: %v", err)
	}
	target := cfg.GetFirstTarget()
	if target.Host != "0.0.0.0:1" {
		t.Errorf("target host: got %q, want \"0.0.0.0:1\"", target.Host)
	}

	cfg, err = NewPortLongLabel("443/https:65535/http")
	if err != nil {
		t.Fatalf("unexpected error for port 65535: %v", err)
	}
	target = cfg.GetFirstTarget()
	if target.Host != "0.0.0.0:65535" {
		t.Errorf("target host: got %q, want \"0.0.0.0:65535\"", target.Host)
	}
}

func TestNewPortShortLabel_ProxyPortOutOfRange(t *testing.T) {
	_, err := NewPortShortLabel("0/tcp")
	if err == nil {
		t.Error("expected error for proxy port 0")
	}

	_, err = NewPortShortLabel("65536/https")
	if err == nil {
		t.Error("expected error for proxy port 65536")
	}
}

func TestPortConfig_GetFirstTarget_Empty(t *testing.T) {
	cfg := PortConfig{}
	target := cfg.GetFirstTarget()
	if target.Scheme != "" || target.Host != "" {
		t.Errorf("empty config: got scheme=%q host=%q, want empty", target.Scheme, target.Host)
	}
}

func TestPortConfig_ReplaceTarget(t *testing.T) {
	cfg, _ := NewPortLongLabel("443/https:80/http")

	original := cfg.GetFirstTarget()
	replacement := urlMustParse("http://mycontainer:8080")

	cfg.ReplaceTarget(original, replacement)

	result := cfg.GetFirstTarget()
	if result.Host != "mycontainer:8080" {
		t.Errorf("after ReplaceTarget: got host %q, want \"mycontainer:8080\"", result.Host)
	}
}

func urlMustParse(s string) *url.URL {
	u, err := url.Parse(s)
	if err != nil {
		panic(err)
	}
	return u
}
