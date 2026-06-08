// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package tailscale

import (
	"context"
	"errors"
	"testing"
)

func TestExposureLookup_NotStarted(t *testing.T) {
	t.Parallel()

	m := map[string]int{"port1": 42}
	_, err := exposureLookup[int](false, m, "port1")
	if !errors.Is(err, errExposureNotStarted) {
		t.Errorf("expected errExposureNotStarted, got %v", err)
	}
}

func TestExposureLookup_NotFound(t *testing.T) {
	t.Parallel()

	m := map[string]int{"port1": 42}
	_, err := exposureLookup[int](true, m, "nonexistent")
	if !errors.Is(err, ErrProxyPortNotFound) {
		t.Errorf("expected ErrProxyPortNotFound, got %v", err)
	}
}

func TestExposureLookup_Found(t *testing.T) {
	t.Parallel()

	m := map[string]int{"port1": 42}
	v, err := exposureLookup[int](true, m, "port1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v != 42 {
		t.Errorf("got %d, want %d", v, 42)
	}
}

func TestExposureLookup_EmptyMap(t *testing.T) {
	t.Parallel()

	m := map[string]string{}
	_, err := exposureLookup[string](true, m, "anything")
	if !errors.Is(err, ErrProxyPortNotFound) {
		t.Errorf("expected ErrProxyPortNotFound, got %v", err)
	}
}

// PerProxyExposure

func TestNewPerProxyExposure(t *testing.T) {
	t.Parallel()

	e := NewPerProxyExposure()
	if e == nil {
		t.Fatal("NewPerProxyExposure returned nil")
	}
	if e.started {
		t.Fatal("new exposure should not be started")
	}
}

func TestPerProxyExposure_GetListener_NotStarted(t *testing.T) {
	t.Parallel()

	e := NewPerProxyExposure()
	_, err := e.GetListener("https")
	if !errors.Is(err, errExposureNotStarted) {
		t.Errorf("expected errExposureNotStarted, got %v", err)
	}
}

func TestPerProxyExposure_GetRawTCPListener_NotStarted(t *testing.T) {
	t.Parallel()

	e := NewPerProxyExposure()
	_, err := e.GetRawTCPListener("tcp")
	if !errors.Is(err, errExposureNotStarted) {
		t.Errorf("expected errExposureNotStarted, got %v", err)
	}
}

func TestPerProxyExposure_GetPacketConn_NotStarted(t *testing.T) {
	t.Parallel()

	e := NewPerProxyExposure()
	_, err := e.GetPacketConn("udp")
	if !errors.Is(err, errExposureNotStarted) {
		t.Errorf("expected errExposureNotStarted, got %v", err)
	}
}

func TestPerProxyExposure_Close_Idempotent(t *testing.T) {
	t.Parallel()

	e := NewPerProxyExposure()
	if err := e.Close(context.Background()); err != nil {
		t.Fatalf("unexpected error on first close: %v", err)
	}
	if err := e.Close(context.Background()); err != nil {
		t.Fatalf("unexpected error on second close: %v", err)
	}
}

// SharedSNIExposure

func TestNewSharedSNIExposure(t *testing.T) {
	t.Parallel()

	e := NewSharedSNIExposure(nil, "example.ts.net")
	if e == nil {
		t.Fatal("NewSharedSNIExposure returned nil")
	}
	if e.started {
		t.Fatal("new exposure should not be started")
	}
	if e.domain != "example.ts.net" {
		t.Errorf("domain = %q, want %q", e.domain, "example.ts.net")
	}
}

func TestSharedSNIExposure_GetListener_NotStarted(t *testing.T) {
	t.Parallel()

	e := NewSharedSNIExposure(nil, "example.ts.net")
	_, err := e.GetListener("https")
	if !errors.Is(err, errExposureNotStarted) {
		t.Errorf("expected errExposureNotStarted, got %v", err)
	}
}

func TestSharedSNIExposure_Close_Idempotent(t *testing.T) {
	t.Parallel()

	e := NewSharedSNIExposure(nil, "example.ts.net")
	if err := e.Close(context.Background()); err != nil {
		t.Fatalf("unexpected error on first close: %v", err)
	}
	if err := e.Close(context.Background()); err != nil {
		t.Fatalf("unexpected error on second close: %v", err)
	}
}

// ServicesVIPExposure

func TestNewServicesVIPExposure(t *testing.T) {
	t.Parallel()

	e := NewServicesVIPExposure(nil, "myservice")
	if e == nil {
		t.Fatal("NewServicesVIPExposure returned nil")
	}
	if e.started {
		t.Fatal("new exposure should not be started")
	}
	if e.serviceName != "myservice" {
		t.Errorf("serviceName = %q, want %q", e.serviceName, "myservice")
	}
}

func TestServicesVIPExposure_GetListener_NotStarted(t *testing.T) {
	t.Parallel()

	e := NewServicesVIPExposure(nil, "myservice")
	_, err := e.GetListener("https")
	if !errors.Is(err, errExposureNotStarted) {
		t.Errorf("expected errExposureNotStarted, got %v", err)
	}
}

func TestServicesVIPExposure_Close_Idempotent(t *testing.T) {
	t.Parallel()

	e := NewServicesVIPExposure(nil, "myservice")
	if err := e.Close(context.Background()); err != nil {
		t.Fatalf("unexpected error on first close: %v", err)
	}
	if err := e.Close(context.Background()); err != nil {
		t.Fatalf("unexpected error on second close: %v", err)
	}
}

func TestServicesVIPExposure_FirstFQDN_Empty(t *testing.T) {
	t.Parallel()

	e := NewServicesVIPExposure(nil, "myservice")
	if fqdn := e.firstFQDN(); fqdn != "" {
		t.Errorf("firstFQDN = %q, want empty", fqdn)
	}
}
