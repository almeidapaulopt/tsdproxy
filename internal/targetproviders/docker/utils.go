// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package docker

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/almeidapaulopt/tsdproxy/internal/config"
)

// getLabelBool method returns a bool from a container label.
func (c *container) getLabelBool(label string, defaultValue bool) bool {
	// Set default value
	value := defaultValue
	if valueString, ok := c.labels[label]; ok {
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

// getLabelString method returns a string from a container label.
func (c *container) getLabelString(label string, defaultValue string) string {
	value := defaultValue
	if valueString, ok := c.labels[label]; ok {
		value = valueString
	}

	return value
}

func (c *container) getLabelInt(label string, defaultValue, minVal, maxVal int) int {
	value := defaultValue
	valueString, ok := c.labels[label]
	if !ok {
		return value
	}
	v, err := strconv.Atoi(valueString)
	if err != nil {
		c.log.Debug().Str("label", label).Str("value", valueString).
			Msg("invalid label value, using default")
		return value
	}
	if v < minVal || v > maxVal {
		c.log.Debug().Str("label", label).Int("value", v).Int("min", minVal).Int("max", maxVal).
			Msg("label value out of range, using default")
		return value
	}
	return v
}

// getAuthKeyFromAuthFile method returns a auth key from a file.
func (c *container) getAuthKeyFromAuthFile(authKey string) (string, error) {
	authKeyFile, ok := c.labels[LabelAuthKeyFile]
	if !ok || authKeyFile == "" {
		return authKey, nil
	}

	resolved, err := config.ValidateKeyFilePath(authKeyFile)
	if err != nil {
		return "", fmt.Errorf("invalid auth key file path: %w", err)
	}

	temp, err := os.ReadFile(resolved)
	if err != nil {
		return "", fmt.Errorf("read auth key from file: %w", err)
	}
	return strings.TrimSpace(string(temp)), nil
}
