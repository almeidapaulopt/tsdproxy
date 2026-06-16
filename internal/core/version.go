// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package core

import (
	"runtime/debug"
	"strings"
)

// version is set at build time via ldflags: -X github.com/almeidapaulopt/tsdproxy/internal/core.version=v1.2.3
var version string

const (
	AppName   = "TSDProxy"
	AppAuthor = "Paulo Almeida <almeidapaulopt@gmail.com>"
)

// VersionInfo holds resolved version metadata.
type VersionInfo struct {
	version  string
	appName  string
	dirty    bool
	resolved bool
}

// NewVersionInfo creates a VersionInfo by resolving the build-time version
// string and VCS dirty status.
func NewVersionInfo() *VersionInfo {
	dirty := resolveDirty()
	v := strings.TrimSpace(version)
	if dirty {
		v += "-dirty"
	}
	if v == "" {
		v = "dev"
	}
	return &VersionInfo{
		version:  v,
		dirty:    dirty,
		appName:  AppName,
		resolved: true,
	}
}

// Version returns the resolved version string.
func (vi *VersionInfo) Version() string {
	return vi.version
}

// IsDirty returns whether the working tree was dirty at build time.
func (vi *VersionInfo) IsDirty() bool {
	return vi.dirty
}

// AppNameVersion returns "AppName-version" (e.g. "TSDProxy-v1.2.3").
func (vi *VersionInfo) AppNameVersion() string {
	return vi.appName + "-" + vi.Version()
}

// defaultVersionInfo is the package-level singleton, resolved once at init.
var defaultVersionInfo = NewVersionInfo()

// GetVersion returns the resolved version string (backward-compatible package-level function).
func GetVersion() string {
	return defaultVersionInfo.Version()
}

// GetIsDirty returns the VCS dirty flag (backward-compatible package-level function).
func GetIsDirty() bool {
	return defaultVersionInfo.IsDirty()
}

// AppNameVersion is the package-level "AppName-version" string.
var AppNameVersion = AppName + "-" + GetVersion()

func resolveDirty() bool {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return false
	}
	for _, v := range bi.Settings {
		if v.Key == "vcs.modified" && v.Value == "true" { //nolint:goconst // build info value, not a domain constant
			return true
		}
	}
	return false
}
