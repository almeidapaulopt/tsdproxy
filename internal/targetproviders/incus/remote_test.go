// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package incus

import (
	"errors"
	"os"
	"testing"

	"github.com/rs/zerolog"

	"github.com/almeidapaulopt/tsdproxy/internal/config"
)

// TestRemoteIncusProvider exercises the provider against a real remote Incus
// daemon. Skipped unless connection details are provided:
//
//	TSDPROXY_INCUS_REMOTE_URL=https://host:8443
//	TSDPROXY_INCUS_CLIENT_CRT=/path/client.crt
//	TSDPROXY_INCUS_CLIENT_KEY=/path/client.key
//	TSDPROXY_INCUS_SERVER_CRT=/path/server.crt (optional)
//
// NOTE: no t.Parallel() — shares a real daemon; avoid concurrent API hammering.
func TestRemoteIncusProvider(t *testing.T) {
	url := os.Getenv("TSDPROXY_INCUS_REMOTE_URL")
	if url == "" {
		t.Skip("TSDPROXY_INCUS_REMOTE_URL not set")
	}

	provider := &config.IncusTargetProviderConfig{
		URL:               url,
		TLSClientCertFile: os.Getenv("TSDPROXY_INCUS_CLIENT_CRT"),
		TLSClientKeyFile:  os.Getenv("TSDPROXY_INCUS_CLIENT_KEY"),
		TLSServerCertFile: os.Getenv("TSDPROXY_INCUS_SERVER_CRT"),
		Project:           "default",
	}

	c, err := New(zerolog.Nop(), "remote-test", provider, false, testAssets)
	if err != nil {
		t.Fatalf("provider New() failed: %v", err)
	}
	defer c.Close()

	// An instance without tsdproxy keys must be rejected by the enable check,
	// proving that connect, trust and GetInstance all work end to end.
	t.Run("NotEnabledInstanceRejected", func(t *testing.T) {
		name := os.Getenv("TSDPROXY_INCUS_TEST_INSTANCE")
		if name == "" {
			t.Skip("TSDPROXY_INCUS_TEST_INSTANCE not set")
		}

		_, err := c.ReResolve(name)
		if !errors.Is(err, ErrInstanceNotEnabled) {
			t.Fatalf("expected ErrInstanceNotEnabled for unlabeled instance, got %v", err)
		}
	})
}
