// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package tailscale

import (
	"context"
	"strings"
	"testing"

	"github.com/almeidapaulopt/tsdproxy/internal/core/secretstring"
)

func TestNewAPIClientFactoryTrimsWhitespace(t *testing.T) {
	t.Parallel()

	f := NewAPIClientFactory("  id  ", secretstring.SecretString("  secret  "))
	if f.clientID != "id" {
		t.Errorf("clientID = %q, want %q", f.clientID, "id")
	}
	if f.clientSecret.Value() != "secret" {
		t.Errorf("clientSecret = %q, want %q", f.clientSecret.Value(), "secret")
	}
}

func TestIsAvailableTrueWhenBothSet(t *testing.T) {
	t.Parallel()

	f := NewAPIClientFactory("client-id", secretstring.SecretString("client-secret"))
	if !f.IsAvailable() {
		t.Error("IsAvailable() = false, want true when both credentials are set")
	}
}

func TestIsAvailableFalseWhenClientIDEmpty(t *testing.T) {
	t.Parallel()

	f := NewAPIClientFactory("", secretstring.SecretString("client-secret"))
	if f.IsAvailable() {
		t.Error("IsAvailable() = true, want false when clientID is empty")
	}
}

func TestIsAvailableFalseWhenSecretEmpty(t *testing.T) {
	t.Parallel()

	f := NewAPIClientFactory("client-id", secretstring.SecretString(""))
	if f.IsAvailable() {
		t.Error("IsAvailable() = true, want false when clientSecret is empty")
	}
}

func TestNewClientReturnsNilWhenNotAvailable(t *testing.T) {
	t.Parallel()

	f := NewAPIClientFactory("", secretstring.SecretString(""))
	client := f.NewClient(ScopeDevices)
	if client != nil {
		t.Error("NewClient() should return nil when not available")
	}
}

func TestNewClientReturnsNonNilWhenAvailable(t *testing.T) {
	t.Parallel()

	f := NewAPIClientFactory("test-id", secretstring.SecretString("test-secret"))
	client := f.NewClient(ScopeDevices, ScopeAuthKeys)
	if client == nil {
		t.Fatal("NewClient() returned nil, want non-nil client")
	}
	if client.Tailnet != "-" {
		t.Errorf("Tailnet = %q, want %q", client.Tailnet, "-")
	}
	if client.UserAgent != userAgent {
		t.Errorf("UserAgent = %q, want %q", client.UserAgent, userAgent)
	}
	if client.Auth == nil {
		t.Fatal("Auth is nil, want OAuth config")
	}
}

func TestAPIClientScopesPerProxy(t *testing.T) {
	t.Parallel()

	scopes := ScopesPerProxy()
	want := []string{ScopeDevices, ScopeAuthKeys}
	if len(scopes) != len(want) {
		t.Fatalf("ScopesPerProxy() returned %d scopes, want %d", len(scopes), len(want))
	}
	for i, s := range scopes {
		if s != want[i] {
			t.Errorf("scopes[%d] = %q, want %q", i, s, want[i])
		}
	}
}

func TestAPIClientScopesServices(t *testing.T) {
	t.Parallel()

	scopes := ScopesServices()
	want := []string{ScopeDevices, ScopeAuthKeys, ScopeServices}
	if len(scopes) != len(want) {
		t.Fatalf("ScopesServices() returned %d scopes, want %d", len(scopes), len(want))
	}
	for i, s := range scopes {
		if s != want[i] {
			t.Errorf("scopes[%d] = %q, want %q", i, s, want[i])
		}
	}
}

func TestValidateAccessReturnsErrorWhenNotConfigured(t *testing.T) {
	t.Parallel()

	f := NewAPIClientFactory("", secretstring.SecretString(""))
	err := f.ValidateAccess(context.Background(), ScopesPerProxy())
	if err == nil {
		t.Fatal("ValidateAccess() should return error when not configured")
	}
	if !strings.Contains(err.Error(), "OAuth credentials not configured") {
		t.Errorf("error = %q, want to contain 'OAuth credentials not configured'", err.Error())
	}
}
