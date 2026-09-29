// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package settings

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/rs/zerolog"
)

func TestBool(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		labels       map[string]string
		label        string
		defaultValue bool
		expected     bool
	}{
		{name: "true label", labels: map[string]string{"enable": "true"}, label: "enable", defaultValue: false, expected: true},
		{name: "false label", labels: map[string]string{"enable": "false"}, label: "enable", defaultValue: true, expected: false},
		{name: "1 as true", labels: map[string]string{"enable": "1"}, label: "enable", defaultValue: false, expected: true},
		{name: "0 as false", labels: map[string]string{"enable": "0"}, label: "enable", defaultValue: true, expected: false},
		{name: "invalid value uses default", labels: map[string]string{"enable": "notabool"}, label: "enable", defaultValue: true, expected: true},
		{name: "missing label uses default (false)", labels: map[string]string{}, label: "enable", defaultValue: false, expected: false},
		{name: "missing label uses default (true)", labels: map[string]string{}, label: "enable", defaultValue: true, expected: true},
		{name: "TRUE case insensitive", labels: map[string]string{"enable": "TRUE"}, label: "enable", defaultValue: false, expected: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := Bool(tt.labels, tt.label, tt.defaultValue)
			if result != tt.expected {
				t.Errorf("Bool(%q, %v) = %v, want %v", tt.label, tt.defaultValue, result, tt.expected)
			}
		})
	}
}

func TestString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		labels       map[string]string
		label        string
		defaultValue string
		expected     string
	}{
		{name: "label exists", labels: map[string]string{"name": "my-service"}, label: "name", defaultValue: "default", expected: "my-service"},
		{name: "label missing", labels: map[string]string{}, label: "name", defaultValue: "default", expected: "default"},
		{name: "empty string label", labels: map[string]string{"name": ""}, label: "name", defaultValue: "default", expected: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := String(tt.labels, tt.label, tt.defaultValue)
			if result != tt.expected {
				t.Errorf("String(%q, %q) = %q, want %q", tt.label, tt.defaultValue, result, tt.expected)
			}
		})
	}
}

func TestInt(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		labels       map[string]string
		label        string
		defaultValue int
		min          int
		max          int
		expected     int
	}{
		{name: "valid in range", labels: map[string]string{"interval": "30"}, label: "interval", defaultValue: 10, min: 1, max: 100, expected: 30},
		{name: "below min returns default", labels: map[string]string{"interval": "0"}, label: "interval", defaultValue: 10, min: 1, max: 100, expected: 10},
		{name: "above max returns default", labels: map[string]string{"interval": "200"}, label: "interval", defaultValue: 10, min: 1, max: 100, expected: 10},
		{name: "not a number returns default", labels: map[string]string{"interval": "abc"}, label: "interval", defaultValue: 10, min: 1, max: 100, expected: 10},
		{name: "missing label returns default", labels: map[string]string{}, label: "interval", defaultValue: 10, min: 1, max: 100, expected: 10},
		{name: "value equals min", labels: map[string]string{"interval": "1"}, label: "interval", defaultValue: 10, min: 1, max: 100, expected: 1},
		{name: "value equals max", labels: map[string]string{"interval": "100"}, label: "interval", defaultValue: 10, min: 1, max: 100, expected: 100},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := Int(zerolog.Nop(), tt.labels, tt.label, tt.defaultValue, tt.min, tt.max)
			if result != tt.expected {
				t.Errorf("Int(%q, %d, %d, %d) = %d, want %d", tt.label, tt.defaultValue, tt.min, tt.max, result, tt.expected)
			}
		})
	}
}

func TestAuthKeyFromFile(t *testing.T) {
	t.Parallel()

	const fileKey = "authkeyfile"

	t.Run("no authkeyfile label", func(t *testing.T) {
		t.Parallel()
		result, err := AuthKeyFromFile(map[string]string{}, fileKey, "tskey-static-key")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.Value() != "tskey-static-key" {
			t.Errorf("got %q, want %q", result.Value(), "tskey-static-key")
		}
	})

	t.Run("empty authkeyfile label", func(t *testing.T) {
		t.Parallel()
		result, err := AuthKeyFromFile(map[string]string{fileKey: ""}, fileKey, "tskey-key")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.Value() != "tskey-key" {
			t.Errorf("got %q, want %q", result.Value(), "tskey-key")
		}
	})

	t.Run("read from valid file", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		keyPath := filepath.Join(dir, "authkey.txt")
		if err := os.WriteFile(keyPath, []byte("tskey-from-file\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		result, err := AuthKeyFromFile(map[string]string{fileKey: keyPath}, fileKey, "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.Value() != "tskey-from-file" {
			t.Errorf("got %q, want %q", result.Value(), "tskey-from-file")
		}
	})

	t.Run("nonexistent file returns error", func(t *testing.T) {
		t.Parallel()
		_, err := AuthKeyFromFile(map[string]string{fileKey: "/nonexistent/path/key.txt"}, fileKey, "")
		if err == nil {
			t.Fatal("expected error for nonexistent file")
		}
	})
}
