// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package tailscale

import "strings"

// RetryPolicy determines which errors are recoverable and how to retry.
type RetryPolicy struct {
	// MaxAttempts is the maximum number of retry attempts (0 = no retry).
	MaxAttempts int
}

// NewRetryPolicy creates a RetryPolicy with sensible defaults.
func NewRetryPolicy() RetryPolicy {
	return RetryPolicy{
		MaxAttempts: 3, //nolint:mnd
	}
}

// IsRecoverable returns true if the error can be retried.
// Non-recoverable errors include: tag permission failures, hardware attestation required.
func (p RetryPolicy) IsRecoverable(err error) bool {
	if err == nil {
		return false
	}

	msg := err.Error()

	if strings.Contains(msg, "invalid or not permitted") {
		return false
	}

	if strings.Contains(msg, "hardware attestation") {
		return false
	}

	return true
}
