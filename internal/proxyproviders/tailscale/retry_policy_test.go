// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package tailscale

import (
	"errors"
	"testing"
	"time"
)

func TestRetryPolicy_Defaults(t *testing.T) {
	t.Parallel()

	p := NewRetryPolicy()

	if p.MaxAttempts != 3 { //nolint:mnd
		t.Errorf("expected MaxAttempts 3, got %d", p.MaxAttempts)
	}
	if p.InitialBackoff != 2*time.Second {
		t.Errorf("expected InitialBackoff 2s, got %v", p.InitialBackoff)
	}
	if p.MaxBackoff != 30*time.Second {
		t.Errorf("expected MaxBackoff 30s, got %v", p.MaxBackoff)
	}
}

func TestNewRetryPolicyFromConfig_CustomValues(t *testing.T) {
	t.Parallel()

	p := NewRetryPolicyFromConfig(5, 500*time.Millisecond, 10*time.Second) //nolint:mnd

	if p.MaxAttempts != 5 { //nolint:mnd
		t.Errorf("expected MaxAttempts 5, got %d", p.MaxAttempts)
	}
	if p.InitialBackoff != 500*time.Millisecond {
		t.Errorf("expected InitialBackoff 500ms, got %v", p.InitialBackoff)
	}
	if p.MaxBackoff != 10*time.Second {
		t.Errorf("expected MaxBackoff 10s, got %v", p.MaxBackoff)
	}
}

func TestNewRetryPolicyFromConfig_ZeroAttempts(t *testing.T) {
	t.Parallel()

	p := NewRetryPolicyFromConfig(0, time.Second, time.Minute)

	if p.MaxAttempts != 0 {
		t.Errorf("expected MaxAttempts 0, got %d", p.MaxAttempts)
	}
}

func TestIsAuthError_Nil(t *testing.T) {
	t.Parallel()

	if IsAuthError(nil) {
		t.Error("expected false for nil error")
	}
}

func TestIsAuthError_AuthError(t *testing.T) {
	t.Parallel()

	err := errors.New("tag is invalid or not permitted by policy")
	if !IsAuthError(err) {
		t.Error("expected true for auth error")
	}
}

func TestIsAuthError_OtherError(t *testing.T) {
	t.Parallel()

	err := errors.New("connection refused")
	if IsAuthError(err) {
		t.Error("expected false for non-auth error")
	}
}

func TestIsRecoverable_Nil(t *testing.T) {
	t.Parallel()

	p := NewRetryPolicy()
	if p.IsRecoverable(nil) {
		t.Error("expected false for nil error")
	}
}

func TestIsRecoverable_AuthError(t *testing.T) {
	t.Parallel()

	p := NewRetryPolicy()
	err := errors.New("tag is invalid or not permitted by policy")
	if p.IsRecoverable(err) {
		t.Error("expected false for auth error")
	}
}

func TestIsRecoverable_HardwareAttestation(t *testing.T) {
	t.Parallel()

	p := NewRetryPolicy()
	err := errors.New("hardware attestation required")
	if p.IsRecoverable(err) {
		t.Error("expected false for hardware attestation error")
	}
}

func TestIsRecoverable_OtherError(t *testing.T) {
	t.Parallel()

	p := NewRetryPolicy()
	err := errors.New("timeout")
	if !p.IsRecoverable(err) {
		t.Error("expected true for generic error")
	}
}
