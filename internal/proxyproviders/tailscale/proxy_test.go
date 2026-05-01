// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package tailscale

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/rs/zerolog"
	"tailscale.com/tsnet"
)

func TestRemoveStaleState_RemovesDatadir(t *testing.T) {
	dir := t.TempDir()
	stateDir := filepath.Join(dir, "node-a")

	// Create the datadir with the typical files inside.
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"tailscaled.state", "tsdproxy.yaml"} {
		if err := os.WriteFile(filepath.Join(stateDir, name), []byte("stale"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	p := &Proxy{
		log:      zerolog.Nop(),
		tsServer: &tsnet.Server{Dir: stateDir},
	}

	p.removeStaleState()

	if _, err := os.Stat(stateDir); !os.IsNotExist(err) {
		t.Errorf("datadir %s should have been removed, got err: %v", stateDir, err)
	}
}

func TestRemoveStaleState_HandlesMissingDatadir(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "does-not-exist")

	p := &Proxy{
		log:      zerolog.Nop(),
		tsServer: &tsnet.Server{Dir: missing},
	}

	// Should not panic or error when datadir doesn't exist.
	p.removeStaleState()
}

func TestRemoveStaleState_NoTSServer(t *testing.T) {
	p := &Proxy{
		log: zerolog.Nop(),
	}

	// Should be a no-op (no panic) when tsServer is nil.
	p.removeStaleState()
}

func TestRemoveStaleState_EmptyDir(t *testing.T) {
	p := &Proxy{
		log:      zerolog.Nop(),
		tsServer: &tsnet.Server{Dir: ""},
	}

	// Should be a no-op (not blow away cwd or root).
	p.removeStaleState()
}
