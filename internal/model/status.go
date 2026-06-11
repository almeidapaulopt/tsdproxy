// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package model

type (
	ProxyStatus int

	ProxyEvent struct {
		ID        string
		Port      string
		AuthURL   string
		Status    ProxyStatus
		OldStatus ProxyStatus
	}
)

const statusStringUnknown = "Unknown"

const (
	ProxyStatusInitializing ProxyStatus = iota
	ProxyStatusStarting
	ProxyStatusAuthenticating
	ProxyStatusRunning
	ProxyStatusStopping
	ProxyStatusStopped
	ProxyStatusError
	ProxyStatusPaused
	ProxyStatusAwaitingApproval
	ProxyStatusAuthFailed
	ProxyStatusDeviceConflict
	ProxyStatusReconciling
)

var proxyStatusStrings = []string{
	"Initializing",
	"Starting",
	"Authenticating",
	"Running",
	"Stopping",
	"Stopped",
	"Error",
	"Paused",
	"AwaitingApproval",
	"AuthFailed",
	"DeviceConflict",
	"Reconciling",
}

func (s ProxyStatus) String() string {
	i := int(s)
	if i < 0 || i >= len(proxyStatusStrings) {
		return statusStringUnknown
	}
	return proxyStatusStrings[i]
}
