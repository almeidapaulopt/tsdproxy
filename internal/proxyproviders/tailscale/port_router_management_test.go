// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package tailscale

import (
	"errors"
	"net"
	"testing"

	"github.com/rs/zerolog"
)

func TestPortRouter_IsEmpty_Initially(t *testing.T) {
	t.Parallel()

	router := NewPortRouter(RouteSNI, zerolog.Nop())
	if !router.IsEmpty() {
		t.Fatal("new router should be empty")
	}
}

func TestPortRouter_IsEmpty_AfterRegister(t *testing.T) {
	t.Parallel()

	router := NewPortRouter(RouteSNI, zerolog.Nop())
	_, err := router.Register("test.example.com")
	if err != nil {
		t.Fatalf("Register failed: %v", err)
	}
	if router.IsEmpty() {
		t.Fatal("router should not be empty after registration")
	}
}

func TestPortRouter_IsEmpty_AfterUnregister(t *testing.T) {
	t.Parallel()

	router := NewPortRouter(RouteSNI, zerolog.Nop())
	_, err := router.Register("test.example.com")
	if err != nil {
		t.Fatalf("Register failed: %v", err)
	}
	router.Unregister("test.example.com")
	if !router.IsEmpty() {
		t.Fatal("router should be empty after unregister")
	}
}

func TestPortRouter_CloseAll_ClosesAllListeners(t *testing.T) {
	t.Parallel()

	router := NewPortRouter(RouteSNI, zerolog.Nop())
	vl1, err := router.Register("first.example.com")
	if err != nil {
		t.Fatalf("Register first failed: %v", err)
	}
	vl2, err := router.Register("second.example.com")
	if err != nil {
		t.Fatalf("Register second failed: %v", err)
	}

	router.CloseAll()

	if !router.IsEmpty() {
		t.Fatal("router should be empty after CloseAll")
	}

	_, err = vl1.Accept()
	if !errors.Is(err, net.ErrClosed) {
		t.Fatalf("Accept after CloseAll should return net.ErrClosed, got: %v", err)
	}
	_, err = vl2.Accept()
	if !errors.Is(err, net.ErrClosed) {
		t.Fatalf("Accept after CloseAll should return net.ErrClosed, got: %v", err)
	}
}

func TestPortRouter_CloseAll_Idempotent(t *testing.T) {
	t.Parallel()

	router := NewPortRouter(RouteSNI, zerolog.Nop())
	router.CloseAll()
	router.CloseAll()

	if !router.IsEmpty() {
		t.Fatal("router should be empty after CloseAll")
	}
}

func TestPortRouter_Register_Duplicate(t *testing.T) {
	t.Parallel()

	router := NewPortRouter(RouteSNI, zerolog.Nop())
	_, err := router.Register("dup.example.com")
	if err != nil {
		t.Fatalf("first Register failed: %v", err)
	}
	_, err = router.Register("dup.example.com")
	if err == nil {
		t.Fatal("expected error for duplicate registration")
	}
}

func TestPortRouter_Unregister_Unknown(t *testing.T) {
	t.Parallel()

	router := NewPortRouter(RouteSNI, zerolog.Nop())
	if router.Unregister("nonexistent.example.com") {
		t.Fatal("Unregister of unknown domain should return false")
	}
}
