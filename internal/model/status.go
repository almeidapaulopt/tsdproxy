// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package model

type (
	ProxyStatus int

	ProxyEvent struct {
		ID      string
		Port    string
		AuthURL string
		Status  ProxyStatus
	}
)

const (
	ProxyStatusInitializing ProxyStatus = iota
	ProxyStatusStarting
	ProxyStatusAuthenticating
	ProxyStatusRunning
	ProxyStatusStopping
	ProxyStatusStopped
	ProxyStatusError
)

var proxyStatusStrings = []string{
	"Initializing",
	"Starting",
	"Authenticating",
	"Running",
	"Stopping",
	"Stopped",
	"Error",
}

func (s *ProxyStatus) String() string {
	i := int(*s)
	if i < 0 || i >= len(proxyStatusStrings) {
		return "Unknown"
	}
	return proxyStatusStrings[i]
}
