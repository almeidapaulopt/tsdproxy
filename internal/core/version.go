//  SPDX-FileCopyrightText: 2024 Paulo Almeida <almeidapaulopt@gmail.com>
//  SPDX-License-Identifier: MIT

package core

import (
	"runtime/debug"
	"strings"
)

var (
	version        string
	realVersion    *string
	isDirty        *bool
	AppNameVersion = AppName + "-" + GetVersion()
)

const (
	AppName   = "TSDProxy"
	AppAuthor = "Paulo Almeida <almeidapaulopt@gmail.com>"
)

func GetVersion() string {
	if realVersion == nil {
		tempVersion := strings.TrimSpace(version)
		if getIsDirty() {
			tempVersion += "-dirty"
		}
		realVersion = &tempVersion
	}
	return *realVersion
}

func getIsDirty() bool {
	if isDirty == nil {
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
	}
	return *isDirty
}
