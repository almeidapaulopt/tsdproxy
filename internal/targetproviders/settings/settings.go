// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

// Package settings provides typed parsing of tsdproxy settings from the
// string maps used by target providers. Docker container labels and Incus
// instance config keys share the same key-to-string-value shape and
// semantics, so the parsing rules live here once instead of in each provider.
package settings

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/rs/zerolog"

	"github.com/almeidapaulopt/tsdproxy/internal/config"
	"github.com/almeidapaulopt/tsdproxy/internal/core/secretstring"
)

// Bool returns the boolean value at key, or defaultValue when the key is
// absent or not a valid bool.
func Bool(m map[string]string, key string, defaultValue bool) bool {
	// Set default value
	value := defaultValue
	if valueString, ok := m[key]; ok {
		valueBool, err := strconv.ParseBool(valueString)
		// set value only if no error
		// if error, keep default
		//
		if err == nil {
			value = valueBool
		}
	}
	return value
}

// String returns the string value at key, or defaultValue when the key is absent.
func String(m map[string]string, key string, defaultValue string) string {
	if value, ok := m[key]; ok {
		return value
	}
	return defaultValue
}

// Int returns the integer value at key bounded to [minVal, maxVal], or
// defaultValue when the key is absent, unparsable, or out of range. Failures
// to parse or bound are logged at debug level with the caller's logger.
func Int(log zerolog.Logger, m map[string]string, key string, defaultValue, minVal, maxVal int) int {
	valueString, ok := m[key]
	if !ok {
		return defaultValue
	}
	v, err := strconv.Atoi(valueString)
	if err != nil {
		log.Debug().Str("key", key).Str("value", valueString).
			Msg("invalid value, using default")
		return defaultValue
	}
	if v < minVal || v > maxVal {
		log.Debug().Str("key", key).Int("value", v).Int("min", minVal).Int("max", maxVal).
			Msg("value out of range, using default")
		return defaultValue
	}
	return v
}

// AuthKeyFromFile returns authKey unchanged unless m names a key file at
// fileKey, in which case the trimmed file contents are returned as a secret.
func AuthKeyFromFile(m map[string]string, fileKey, authKey string) (secretstring.SecretString, error) {
	authKeyFile, ok := m[fileKey]
	if !ok || authKeyFile == "" {
		return secretstring.SecretString(authKey), nil
	}

	resolved, err := config.ValidateKeyFilePath(authKeyFile)
	if err != nil {
		return "", fmt.Errorf("invalid auth key file path: %w", err)
	}

	temp, err := os.ReadFile(resolved)
	if err != nil {
		return "", fmt.Errorf("read auth key from file: %w", err)
	}
	defer clear(temp)
	return secretstring.SecretString(strings.TrimSpace(string(temp))), nil
}
