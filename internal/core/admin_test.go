// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package core

import (
	"testing"

	"github.com/almeidapaulopt/tsdproxy/internal/model"
)

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
		{"172.32.x not private", "172.32.0.1:8080", false},
		{"172.15.x not private", "172.15.0.1:8080", false},
		{"no port loopback", "127.0.0.1", true},
		{"no port docker bridge", "172.17.0.1", true},
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
