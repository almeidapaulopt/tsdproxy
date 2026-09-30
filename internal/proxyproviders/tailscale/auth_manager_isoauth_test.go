// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package tailscale

import (
	"testing"

	"github.com/rs/zerolog"
)

func TestIsOAuth_NilFactory(t *testing.T) {
	t.Parallel()

	m := NewAuthManager(zerolog.Nop(), nil, false)
	if m.IsOAuth() {
		t.Fatal("IsOAuth() with nil factory should return false")
	}
}

func TestIsOAuth_UnavailableFactory(t *testing.T) {
	t.Parallel()

	f := NewAPIClientFactory("", "")
	m := NewAuthManager(zerolog.Nop(), f, false)
	if m.IsOAuth() {
		t.Fatal("IsOAuth() with unavailable factory should return false")
	}
}

func TestIsOAuth_AvailableFactory(t *testing.T) {
	t.Parallel()

	f := NewAPIClientFactory("client-id", "client-secret")
	m := NewAuthManager(zerolog.Nop(), f, false)
	if !m.IsOAuth() {
		t.Fatal("IsOAuth() with available factory should return true")
	}
}

func TestIsOAuth_EmptyIDOnly(t *testing.T) {
	t.Parallel()

	f := NewAPIClientFactory("", "client-secret")
	m := NewAuthManager(zerolog.Nop(), f, false)
	if m.IsOAuth() {
		t.Fatal("IsOAuth() with empty client ID should return false")
	}
}

func TestIsOAuth_EmptySecretOnly(t *testing.T) {
	t.Parallel()

	f := NewAPIClientFactory("client-id", "")
	m := NewAuthManager(zerolog.Nop(), f, false)
	if m.IsOAuth() {
		t.Fatal("IsOAuth() with empty client secret should return false")
	}
}
