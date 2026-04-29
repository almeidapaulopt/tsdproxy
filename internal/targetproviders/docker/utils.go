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
	// Set default value
	value := defaultValue
	if valueString, ok := c.labels[label]; ok {
		value = valueString
	}

	return value
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
