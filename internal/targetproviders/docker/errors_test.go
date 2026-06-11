// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package docker

import (
	"testing"
)

func TestNoValidTargetFoundError_Error(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		container   string
		expectedMsg string
	}{
		{
			name:        "with container name",
			container:   "myapp",
			expectedMsg: "no valid target found for myapp",
		},
		{
			name:        "empty container name",
			container:   "",
			expectedMsg: "no valid target found for ",
		},
		{
			name:        "container name with special chars",
			container:   "my-app_123",
			expectedMsg: "no valid target found for my-app_123",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := &NoValidTargetFoundError{containerName: tt.container}
			if got := err.Error(); got != tt.expectedMsg {
				t.Fatalf("NoValidTargetFoundError.Error() = %q, want %q", got, tt.expectedMsg)
			}
		})
	}
}
