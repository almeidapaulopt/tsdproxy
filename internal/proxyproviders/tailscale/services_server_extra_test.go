// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package tailscale

import (
	"errors"
	"net/http"
	"testing"

	"github.com/rs/zerolog"

	"github.com/almeidapaulopt/tsdproxy/internal/model"
)

// --- isLocalhost ---

func TestIsLocalhost_True(t *testing.T) {
	t.Parallel()

	if !isLocalhost("127.0.0.1:8080") {
		t.Fatal("isLocalhost should return true for 127.0.0.1")
	}
	if !isLocalhost("[::1]:8080") {
		t.Fatal("isLocalhost should return true for [::1]")
	}
	if !isLocalhost("127.0.0.1") {
		t.Fatal("isLocalhost should return true for bare 127.0.0.1")
	}
}

func TestIsLocalhost_False(t *testing.T) {
	t.Parallel()

	if isLocalhost("100.64.0.1:443") {
		t.Fatal("isLocalhost should return false for Tailscale IP")
	}
	if isLocalhost("10.0.0.1:8080") {
		t.Fatal("isLocalhost should return false for private IP")
	}
}

func TestIsLocalhost_Empty(t *testing.T) {
	t.Parallel()

	if isLocalhost("") {
		t.Fatal("isLocalhost should return false for empty string")
	}
}

// --- isNotFound ---

func TestIsNotFound_Nil(t *testing.T) {
	t.Parallel()

	if isNotFound(nil) {
		t.Fatal("isNotFound(nil) should return false")
	}
}

func TestIsNotFound_NotFound(t *testing.T) {
	t.Parallel()

	if !isNotFound(errors.New("resource not found")) {
		t.Fatal("isNotFound should return true for 'not found' error")
	}
	if !isNotFound(errors.New("404 page not found")) {
		t.Fatal("isNotFound should return true for 404 error")
	}
}

func TestIsNotFound_Other(t *testing.T) {
	t.Parallel()

	if isNotFound(errors.New("internal server error")) {
		t.Fatal("isNotFound should return false for other errors")
	}
	if isNotFound(errors.New("timeout")) {
		t.Fatal("isNotFound should return false for timeout errors")
	}
}

// --- isNameInUseError ---

func TestIsNameInUseError_Nil(t *testing.T) {
	t.Parallel()

	if isNameInUseError(nil) {
		t.Fatal("isNameInUseError(nil) should return false")
	}
}

func TestIsNameInUseError_NameInUse(t *testing.T) {
	t.Parallel()

	if !isNameInUseError(errors.New("name is in use")) {
		t.Fatal("isNameInUseError should return true for 'name is in use'")
	}
	if !isNameInUseError(errors.New("409 conflict: name is in use")) {
		t.Fatal("isNameInUseError should return true for 409")
	}
}

func TestIsNameInUseError_Other(t *testing.T) {
	t.Parallel()

	if isNameInUseError(errors.New("not found")) {
		t.Fatal("isNameInUseError should return false for 'not found'")
	}
	if isNameInUseError(errors.New("unauthorized")) {
		t.Fatal("isNameInUseError should return false for other errors")
	}
}

// --- validCertDomains ---

func TestValidCertDomains_NilTsServer(t *testing.T) {
	t.Parallel()

	ss := NewServicesServer(ServicesServerConfig{
		Hostname: "test-server",
		Log:      zerolog.Nop(),
	})
	defer ss.Close()

	result := ss.validCertDomains(&servicesRuntime{tsServer: nil})
	if result == nil {
		t.Fatal("validCertDomains with nil tsServer should return empty map, not nil")
	}
	if len(result) != 0 {
		t.Fatalf("expected empty map, got %d entries", len(result))
	}
}

// --- reconcileServiceHostname ---

func TestReconcileServiceHostname_NoPrefix(t *testing.T) {
	t.Parallel()

	ss := NewServicesServer(ServicesServerConfig{
		Hostname: "test-server",
		Log:      zerolog.Nop(),
	})
	defer ss.Close()

	// With no device reconciler set, this should simply return without error.
	// The method is a no-op for services without the "svc:" prefix.
	ss.reconcileServiceHostname("plain-service")
}

func TestReconcileServiceHostname_NoReconciler(t *testing.T) {
	t.Parallel()

	ss := NewServicesServer(ServicesServerConfig{
		Hostname: "test-server",
		Log:      zerolog.Nop(),
	})
	defer ss.Close()

	// With svc: prefix but no deviceReconciler, should log a debug message and return.
	ss.reconcileServiceHostname("svc:myapp")
}

// --- trustedPeerIP ---

func TestTrustedPeerIP_NonLocalhostRemote(t *testing.T) {
	t.Parallel()

	ss := NewServicesServer(ServicesServerConfig{
		Hostname: "test-server",
		Log:      zerolog.Nop(),
	})
	defer ss.Close()

	r := &http.Request{
		RemoteAddr: "100.64.0.1:443",
	}

	ip := ss.trustedPeerIP(r)
	if ip != "100.64.0.1" {
		t.Fatalf("expected 100.64.0.1, got %q", ip)
	}
}

func TestTrustedPeerIP_LocalhostNoXFF(t *testing.T) {
	t.Parallel()

	ss := NewServicesServer(ServicesServerConfig{
		Hostname: "test-server",
		Log:      zerolog.Nop(),
	})
	defer ss.Close()

	r := &http.Request{
		RemoteAddr: "127.0.0.1:8080",
		Header:     http.Header{},
	}

	ip := ss.trustedPeerIP(r)
	if ip != "" {
		t.Fatalf("expected empty IP (no XFF), got %q", ip)
	}
}

func TestTrustedPeerIP_LocalhostMultipleXFF(t *testing.T) {
	t.Parallel()

	ss := NewServicesServer(ServicesServerConfig{
		Hostname: "test-server",
		Log:      zerolog.Nop(),
	})
	defer ss.Close()

	r := &http.Request{
		RemoteAddr: "127.0.0.1:8080",
		Header: http.Header{
			"X-Forwarded-For": []string{"10.0.0.1", "10.0.0.2"},
		},
	}

	ip := ss.trustedPeerIP(r)
	if ip != "" {
		t.Fatalf("expected empty IP (multiple XFF headers), got %q", ip)
	}
}

func TestTrustedPeerIP_LocalhostXFFChain(t *testing.T) {
	t.Parallel()

	ss := NewServicesServer(ServicesServerConfig{
		Hostname: "test-server",
		Log:      zerolog.Nop(),
	})
	defer ss.Close()

	r := &http.Request{
		RemoteAddr: "127.0.0.1:8080",
		Header: http.Header{
			"X-Forwarded-For": []string{"10.0.0.1, 10.0.0.2"},
		},
	}

	ip := ss.trustedPeerIP(r)
	if ip != "" {
		t.Fatalf("expected empty IP (comma chain), got %q", ip)
	}
}

func TestTrustedPeerIP_LocalhostValidXFF(t *testing.T) {
	t.Parallel()

	ss := NewServicesServer(ServicesServerConfig{
		Hostname: "test-server",
		Log:      zerolog.Nop(),
	})
	defer ss.Close()

	r := &http.Request{
		RemoteAddr: "127.0.0.1:8080",
		Header: http.Header{
			"X-Forwarded-For": []string{"100.64.0.1"},
		},
	}

	ip := ss.trustedPeerIP(r)
	if ip != "100.64.0.1" {
		t.Fatalf("expected 100.64.0.1, got %q", ip)
	}
}

func TestTrustedPeerIP_LocalhostXFFLoopback(t *testing.T) {
	t.Parallel()

	ss := NewServicesServer(ServicesServerConfig{
		Hostname: "test-server",
		Log:      zerolog.Nop(),
	})
	defer ss.Close()

	r := &http.Request{
		RemoteAddr: "127.0.0.1:8080",
		Header: http.Header{
			"X-Forwarded-For": []string{"127.0.0.1"},
		},
	}

	ip := ss.trustedPeerIP(r)
	if ip != "" {
		t.Fatalf("expected empty IP (loopback XFF is rejected), got %q", ip)
	}
}

func TestTrustedPeerIP_LocalhostXFFEmpty(t *testing.T) {
	t.Parallel()

	ss := NewServicesServer(ServicesServerConfig{
		Hostname: "test-server",
		Log:      zerolog.Nop(),
	})
	defer ss.Close()

	r := &http.Request{
		RemoteAddr: "127.0.0.1:8080",
		Header: http.Header{
			"X-Forwarded-For": []string{""},
		},
	}

	ip := ss.trustedPeerIP(r)
	if ip != "" {
		t.Fatalf("expected empty IP (empty XFF value), got %q", ip)
	}
}

func TestTrustedPeerIP_IPv6NonLocalhost(t *testing.T) {
	t.Parallel()

	ss := NewServicesServer(ServicesServerConfig{
		Hostname: "test-server",
		Log:      zerolog.Nop(),
	})
	defer ss.Close()

	r := &http.Request{
		RemoteAddr: "[fd7a:115c:a1e0::1]:443",
	}

	ip := ss.trustedPeerIP(r)
	if ip != "fd7a:115c:a1e0::1" {
		t.Fatalf("expected fd7a:115c:a1e0::1, got %q", ip)
	}
}

func TestNormalizeIPFromTrustedPeerIP(t *testing.T) {
	t.Parallel()

	// Verify model.NormalizeIP works correctly for the patterns used by trustedPeerIP.
	if got := model.NormalizeIP("100.64.0.1:443"); got != "100.64.0.1" {
		t.Fatalf("NormalizeIP(100.64.0.1:443) = %q, want 100.64.0.1", got)
	}
}
