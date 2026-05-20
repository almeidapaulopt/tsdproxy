// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package core

import (
	"runtime/debug"
	"strings"
	"sync"
)

var (
	version     string
	realVersion *string
	dirtyFlag   bool
	versionOnce sync.Once

	AppNameVersion = AppName + "-" + GetVersion()
)

const (
	AppName   = "TSDProxy"
	AppAuthor = "Paulo Almeida <almeidapaulopt@gmail.com>"
)

func GetVersion() string {
	versionOnce.Do(func() {
		dirtyFlag = resolveDirty()
		tempVersion := strings.TrimSpace(version)
		if dirtyFlag {
			tempVersion += "-dirty"
		}
		if tempVersion == "" {
			tempVersion = "dev"
		}
		realVersion = &tempVersion
	})
	return *realVersion
}

func GetIsDirty() bool {
	GetVersion()
	return dirtyFlag
}

func resolveDirty() bool {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return false
	}
	for _, v := range bi.Settings {
		if v.Key == "vcs.modified" && v.Value == "true" {
			return true
		}
	}
	return false
}
