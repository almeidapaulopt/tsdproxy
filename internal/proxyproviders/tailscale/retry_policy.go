// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package tailscale

import (
	"strings"
	"time"
)

// RetryPolicy determines which errors are recoverable and how to retry.
type RetryPolicy struct {
	// MaxAttempts is the maximum number of retry attempts (0 = no retry).
	MaxAttempts int
	// InitialBackoff is the base backoff duration between retry attempts.
	InitialBackoff time.Duration
	// MaxBackoff is the maximum backoff duration cap for exponential growth.
	MaxBackoff time.Duration
}

// NewRetryPolicy creates a RetryPolicy with sensible defaults.
func NewRetryPolicy() RetryPolicy {
	return RetryPolicy{
		MaxAttempts:    3,                //nolint:mnd
		InitialBackoff: 2 * time.Second,  //nolint:mnd
		MaxBackoff:     30 * time.Second, //nolint:mnd
	}
}

// NewRetryPolicyFromConfig creates a RetryPolicy from config values.
// If maxAttempts is 0, retry is disabled.
func NewRetryPolicyFromConfig(maxAttempts int, initialBackoff, maxBackoff time.Duration) RetryPolicy {
	return RetryPolicy{
		MaxAttempts:    maxAttempts,
		InitialBackoff: initialBackoff,
		MaxBackoff:     maxBackoff,
	}
}

// IsAuthError returns true if the error indicates an authentication or
// tag permission failure from the Tailscale API.
func IsAuthError(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "invalid or not permitted")
}

// IsRecoverable returns true if the error can be retried.
// Non-recoverable errors include: tag permission failures, hardware attestation required.
func (p RetryPolicy) IsRecoverable(err error) bool {
	if err == nil {
		return false
	}

	if IsAuthError(err) {
		return false
	}

	if strings.Contains(err.Error(), "hardware attestation") {
		return false
	}

	return true
}
