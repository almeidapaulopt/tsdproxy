// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package model

import (
	"testing"
)

func TestProxyStatusString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		want   string
		status ProxyStatus
	}{
		{ProxyStatusInitializing, "Initializing"},
		{ProxyStatusStarting, "Starting"},
		{ProxyStatusAuthenticating, "Authenticating"},
		{ProxyStatusRunning, "Running"},
		{ProxyStatusStopping, "Stopping"},
		{ProxyStatusStopped, "Stopped"},
		{ProxyStatusError, "Error"},
		{ProxyStatusPaused, "Paused"},
		{ProxyStatusAwaitingApproval, "AwaitingApproval"},
		{ProxyStatusAuthFailed, "AuthFailed"},
		{ProxyStatusDeviceConflict, "DeviceConflict"},
		{ProxyStatusReconciling, "Reconciling"},
		{ProxyStatus(-1), "Unknown"},
		{ProxyStatus(999), "Unknown"},
	}

	for _, tc := range tests {
		t.Run(tc.want, func(t *testing.T) {
			t.Parallel()

			got := tc.status.String()
			if got != tc.want {
				t.Errorf("ProxyStatus(%d).String() = %q, want %q", tc.status, got, tc.want)
			}
		})
	}
}
