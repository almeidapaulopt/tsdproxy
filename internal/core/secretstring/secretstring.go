// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package secretstring

import (
	"encoding"
	"fmt"
)

// SecretString wraps a string value that should never appear in logs,
// error messages, or serialized output. Use it for API tokens, auth
// keys, and other credentials.
//
// All formatting and serialization methods return "[REDACTED]" instead
// of the underlying value. Access the raw value via the Value() method.
type SecretString string

var (
	_ fmt.Stringer           = SecretString("")
	_ encoding.TextMarshaler = SecretString("")
)

// Value returns the underlying secret string.
func (s SecretString) Value() string { return string(s) }

// String returns "[REDACTED]" to prevent accidental logging.
func (SecretString) String() string { return "[REDACTED]" }

// GoString returns "[REDACTED]" to prevent accidental logging in %#v format.
func (SecretString) GoString() string { return "[REDACTED]" }

// MarshalText returns "[REDACTED]" to prevent accidental serialization.
func (SecretString) MarshalText() ([]byte, error) { return []byte("[REDACTED]"), nil }

// Format implements fmt.Formatter, returning "[REDACTED]" for all verbs.
func (SecretString) Format(f fmt.State, _ rune) { f.Write([]byte("[REDACTED]")) } //nolint:errcheck
