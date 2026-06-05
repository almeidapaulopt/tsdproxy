// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package secretstring

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSecretString_Value(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		s    SecretString
		want string
	}{
		{name: "simple value", s: SecretString("hello"), want: "hello"},
		{name: "empty", s: SecretString(""), want: ""},
		{name: "api key", s: SecretString("tskey-auth-abcdef123456"), want: "tskey-auth-abcdef123456"},
		{name: "special chars", s: SecretString("p@ssw0rd!#$"), want: "p@ssw0rd!#$"},
		{name: "newlines", s: SecretString("line1\nline2"), want: "line1\nline2"},
		{name: "unicode", s: SecretString("sëcrët"), want: "sëcrët"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, tt.s.Value())
		})
	}
}

func TestSecretString_String(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		s    SecretString
	}{
		{name: "simple value", s: SecretString("hello")},
		{name: "empty", s: SecretString("")},
		{name: "api key", s: SecretString("tskey-auth-abcdef123456")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, "[REDACTED]", tt.s.String())
		})
	}
}

func TestSecretString_GoString(t *testing.T) {
	t.Parallel()

	s := SecretString("super-secret-value")
	assert.Equal(t, "[REDACTED]", s.GoString())

	// Also verify via %#v formatting (Go calls GoString for %#v)
	result := fmt.Sprintf("%#v", s)
	assert.Equal(t, "[REDACTED]", result)
}

func TestSecretString_MarshalText(t *testing.T) {
	t.Parallel()

	s := SecretString("tskey-auth-secret123")

	data, err := s.MarshalText()
	require.NoError(t, err)
	assert.Equal(t, "[REDACTED]", string(data))
}

func TestSecretString_MarshalJSON(t *testing.T) {
	t.Parallel()

	type config struct {
		APIKey SecretString `json:"apiKey"`
		Name   string       `json:"name"`
	}

	cfg := config{
		APIKey: SecretString("tskey-auth-secret123"),
		Name:   "test",
	}

	data, err := json.Marshal(cfg) //nolint:gosec // G117: intentionally testing SecretString marshaling
	require.NoError(t, err)

	jsonStr := string(data)
	assert.Contains(t, jsonStr, `"apiKey":"[REDACTED]"`)
	assert.NotContains(t, jsonStr, "tskey-auth-secret123")
	assert.Contains(t, jsonStr, `"name":"test"`)
}

func TestSecretString_Format(t *testing.T) {
	t.Parallel()

	s := SecretString("super-secret-value")

	tests := []struct {
		name   string
		format string
	}{
		{name: "%s", format: "%s"},
		{name: "%v", format: "%v"},
		{name: "%+v", format: "%+v"},
		{name: "%q", format: "%q"},
		{name: "%x", format: "%x"},
		{name: "%X", format: "%X"},
		{name: "%t", format: "%t"},
		{name: "%d", format: "%d"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := fmt.Sprintf(tt.format, s)
			assert.Equal(t, "[REDACTED]", result, "format verb %q must not leak secret", tt.format)
		})
	}
}

// SECURITY: SecretString must never expose the underlying value through any
// formatting path. A leaked auth key in logs is a critical vulnerability.
func TestSecretString_ValueNeverLeaks(t *testing.T) {
	t.Parallel()

	secret := "tskey-auth-DO-NOT-LEAK-12345" //nolint:gosec // G101: test fixture, not a real credential
	s := SecretString(secret)

	// String()
	assert.NotContains(t, s.String(), secret)

	// GoString()
	assert.NotContains(t, s.GoString(), secret)

	// MarshalText()
	data, err := s.MarshalText()
	require.NoError(t, err)
	assert.NotContains(t, string(data), secret)

	// All fmt format verbs
	for _, verb := range []string{"%s", "%v", "%+v", "%#v", "%q", "%x", "%X"} {
		result := fmt.Sprintf(verb, s)
		assert.NotContains(t, result, secret, "format verb %q leaked secret value", verb)
	}

	// JSON serialization
	type wrapper struct {
		Token SecretString `json:"token"`
	}
	w := wrapper{Token: s}
	jsonData, err := json.Marshal(w)
	require.NoError(t, err)
	assert.NotContains(t, string(jsonData), secret)

	// JSON marshaling directly on the SecretString
	rawJSON, err := json.Marshal(s)
	require.NoError(t, err)
	assert.NotContains(t, string(rawJSON), secret)

	// But Value() DOES return the real value (that's its purpose)
	assert.Equal(t, secret, s.Value())
}
