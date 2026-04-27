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
	isDirty     *bool
	versionOnce sync.Once

	AppNameVersion = AppName + "-" + GetVersion()
)

const (
	AppName   = "TSDProxy"
	AppAuthor = "Paulo Almeida <almeidapaulopt@gmail.com>"
)

func GetVersion() string {
	versionOnce.Do(func() {
		tempVersion := strings.TrimSpace(version)
		if getIsDirty() {
			tempVersion += "-dirty"
		}
		realVersion = &tempVersion
	})
	if realVersion == nil {
		return "dev"
	}
	return *realVersion
}

func getIsDirty() bool {
	if isDirty != nil {
		return *isDirty
	}

	bi, ok := debug.ReadBuildInfo()
	if ok {
		modified := false

		for _, v := range bi.Settings {
			if v.Key == "vcs.modified" {
				if v.Value == "true" {
					modified = true
				}
			}
		}
		isDirty = &modified
	}
	if isDirty == nil {
		return false
	}
	return *isDirty
}
