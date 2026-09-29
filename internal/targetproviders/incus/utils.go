// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package incus

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/almeidapaulopt/tsdproxy/internal/config"
	"github.com/almeidapaulopt/tsdproxy/internal/core/secretstring"
)

// getConfigBool method returns a bool from an instance config key.
func (i *instance) getConfigBool(key string, defaultValue bool) bool {
	// Set default value
	value := defaultValue
	if valueString, ok := i.config[key]; ok {
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

// getConfigString method returns a string from an instance config key.
func (i *instance) getConfigString(key string, defaultValue string) string {
	value := defaultValue
	if valueString, ok := i.config[key]; ok {
		value = valueString
	}

	return value
}

// getConfigInt method returns an int from an instance config key, bounded by
// minVal and maxVal. Falls back to defaultValue on parse or range errors.
func (i *instance) getConfigInt(key string, defaultValue, minVal, maxVal int) int {
	value := defaultValue
	valueString, ok := i.config[key]
	if !ok {
		return value
	}
	v, err := strconv.Atoi(valueString)
	if err != nil {
		i.log.Debug().Str("key", key).Str("value", valueString).
			Msg("invalid config value, using default")
		return value
	}
	if v < minVal || v > maxVal {
		i.log.Debug().Str("key", key).Int("value", v).Int("min", minVal).Int("max", maxVal).
			Msg("config value out of range, using default")
		return value
	}
	return v
}

// getAuthKeyFromAuthFile method returns an auth key from a file.
func (i *instance) getAuthKeyFromAuthFile(authKey string) (secretstring.SecretString, error) {
	authKeyFile, ok := i.config[ConfigAuthKeyFile]
	if !ok || authKeyFile == "" {
		return secretstring.SecretString(authKey), nil
	}

	resolved, err := config.ValidateKeyFilePath(authKeyFile)
	if err != nil {
		return "", fmt.Errorf("invalid auth key file path: %w", err)
	}

	temp, err := os.ReadFile(resolved) //nolint:gosec // G703: path is validated by ValidateKeyFilePath
	if err != nil {
		return "", fmt.Errorf("read auth key from file: %w", err)
	}
	defer clear(temp)
	return secretstring.SecretString(strings.TrimSpace(string(temp))), nil
}

// instanceEnabled reports whether the instance config enables tsdproxy.
func instanceEnabled(configMap map[string]string) bool {
	value, ok := configMap[ConfigIsEnabled]
	if !ok {
		return false
	}
	enabled, err := strconv.ParseBool(value)
	return err == nil && enabled
}
