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
		{"Initializing", ProxyStatusInitializing},
		{"Starting", ProxyStatusStarting},
		{"Authenticating", ProxyStatusAuthenticating},
		{"Running", ProxyStatusRunning},
		{"Stopping", ProxyStatusStopping},
		{"Stopped", ProxyStatusStopped},
		{"Error", ProxyStatusError},
		{"Paused", ProxyStatusPaused},
		{"AwaitingApproval", ProxyStatusAwaitingApproval},
		{"AuthFailed", ProxyStatusAuthFailed},
		{"DeviceConflict", ProxyStatusDeviceConflict},
		{"Reconciling", ProxyStatusReconciling},
		{"Unknown", ProxyStatus(-1)},
		{"Unknown", ProxyStatus(999)},
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
