// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package core

import (
	"strings"
	"testing"
)

func TestGetVersion(t *testing.T) {
	t.Parallel()

	v := GetVersion()
	if v == "" {
		t.Fatal("GetVersion() returned empty string, want non-empty")
	}
}

func TestGetVersionReturnsDev(t *testing.T) {
	t.Parallel()

	v := GetVersion()
	if v == "" {
		t.Fatal("GetVersion() returned empty string, want non-empty")
	}
	// Acceptable values: "dev", "dev-dirty", "v1.2.3", "v1.2.3-dirty", etc.
	if !strings.Contains(v, "dev") && !strings.Contains(v, ".") && !strings.Contains(v, "-dirty") {
		t.Errorf("GetVersion() = %q, expected to contain 'dev', a version number, or '-dirty'", v)
	}
}

func TestGetIsDirty(t *testing.T) {
	t.Parallel()

	isDirty := GetIsDirty()
	v := GetVersion()
	hasDirtySuffix := strings.HasSuffix(v, "-dirty")
	if isDirty && !hasDirtySuffix {
		t.Errorf("GetIsDirty() = true but GetVersion() = %q has no -dirty suffix", v)
	}
}

func TestAppNameVersion(t *testing.T) {
	t.Parallel()

	if AppNameVersion == "" {
		t.Fatal("AppNameVersion is empty, want non-empty")
	}
	if !strings.HasPrefix(AppNameVersion, AppName+"-") {
		t.Errorf("AppNameVersion = %q, want prefix %q", AppNameVersion, AppName+"-")
	}
}
