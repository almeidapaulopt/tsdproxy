// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package model

import (
	"context"
	"testing"
)

func TestWhoisFromContext_NoValue(t *testing.T) {
	t.Parallel()

	who, ok := WhoisFromContext(context.Background())
	if ok {
		t.Fatal("expected ok to be false for empty context")
	}
	if who != (Whois{}) {
		t.Fatalf("expected zero Whois, got %+v", who)
	}
}

func TestWhoisFromContext_WithValue(t *testing.T) {
	t.Parallel()

	want := Whois{
		ID:            "user-123",
		DisplayName:   "Test User",
		Username:      "testuser",
		ProfilePicURL: "https://example.com/pic.png",
	}
	ctx := WhoisNewContext(context.Background(), want)

	got, ok := WhoisFromContext(ctx)
	if !ok {
		t.Fatal("expected ok to be true")
	}
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestWhoisNewContext(t *testing.T) {
	t.Parallel()

	w := Whois{ID: "abc", DisplayName: "Alice"}
	ctx := WhoisNewContext(context.Background(), w)

	retrieved, ok := ctx.Value(ContextKeyWhois).(Whois)
	if !ok {
		t.Fatal("expected to retrieve Whois from context")
	}
	if retrieved != w {
		t.Fatalf("got %+v, want %+v", retrieved, w)
	}
}

func TestIsLocalhost(t *testing.T) {
	t.Parallel()

	tests := []struct {
		addr string
		want bool
	}{
		{"127.0.0.1:12345", true},
		{"[::1]:8080", true},
		{"192.168.1.1:8080", false},
		{"10.0.0.1:80", false},
		{"127.0.0.1", true},
		{"", false},
	}

	for _, tc := range tests {
		t.Run(tc.addr, func(t *testing.T) {
			t.Parallel()

			got := IsLocalhost(tc.addr)
			if got != tc.want {
				t.Errorf("IsLocalhost(%q) = %v, want %v", tc.addr, got, tc.want)
			}
		})
	}
}

func TestNormalizeIP(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input string
		want  string
	}{
		{"1.2.3.4:443", "1.2.3.4"},
		{"[::1]:8080", "::1"},
		{"10.0.0.1", "10.0.0.1"},
		{"  1.2.3.4:80  ", "1.2.3.4"},
		{"not-an-ip", ""},
		{"", ""},
		{"::1", "::1"},
		{"fe80::1", "fe80::1"},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			t.Parallel()

			got := NormalizeIP(tc.input)
			if got != tc.want {
				t.Errorf("NormalizeIP(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}
