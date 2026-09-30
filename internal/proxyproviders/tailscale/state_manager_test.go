// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package tailscale

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newStateManager(t *testing.T) *StateManager {
	t.Helper()
	return NewStateManager(zerolog.Nop())
}

func touchStateFile(t *testing.T, datadir string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(datadir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(datadir, "tailscaled.state"), []byte("{}"), 0o600))
}

func writeMetaFile(t *testing.T, datadir string, meta *stateMeta) {
	t.Helper()
	sm := NewStateManager(zerolog.Nop())
	sm.Save(&NodeConfig{Ephemeral: meta.Ephemeral}, datadir)
}

// --- StateExists ---

func TestStateExists_NoState(t *testing.T) {
	t.Parallel()

	sm := newStateManager(t)
	dir := t.TempDir()

	assert.False(t, sm.StateExists(dir))
}

func TestStateExists_WithState(t *testing.T) {
	t.Parallel()

	sm := newStateManager(t)
	dir := t.TempDir()
	touchStateFile(t, dir)

	assert.True(t, sm.StateExists(dir))
}

func TestStateExists_DirNotFile(t *testing.T) {
	t.Parallel()

	sm := newStateManager(t)
	dir := t.TempDir()

	stateDir := filepath.Join(dir, "tailscaled.state")
	require.NoError(t, os.MkdirAll(stateDir, 0o750))

	assert.False(t, sm.StateExists(dir))
}

// --- CleanStale ---

func TestCleanStale_NoStateFile(t *testing.T) {
	t.Parallel()

	sm := newStateManager(t)
	dir := t.TempDir()

	result := sm.CleanStale(&NodeConfig{Ephemeral: false}, dir)
	assert.False(t, result)
}

func TestCleanStale_NoMetaFile(t *testing.T) {
	t.Parallel()

	sm := newStateManager(t)
	dir := t.TempDir()
	touchStateFile(t, dir)

	result := sm.CleanStale(&NodeConfig{Ephemeral: false}, dir)
	assert.False(t, result)
}

func TestCleanStale_SameEphemeral_NoCleanup(t *testing.T) {
	t.Parallel()

	sm := newStateManager(t)
	dir := t.TempDir()
	touchStateFile(t, dir)
	writeMetaFile(t, dir, &stateMeta{Ephemeral: false})

	result := sm.CleanStale(&NodeConfig{Ephemeral: false}, dir)
	assert.False(t, result)
	assert.FileExists(t, filepath.Join(dir, "tailscaled.state"))
}

func TestCleanStale_EphemeralChanged_Cleanup(t *testing.T) {
	t.Parallel()

	sm := newStateManager(t)
	dir := t.TempDir()
	touchStateFile(t, dir)
	writeMetaFile(t, dir, &stateMeta{Ephemeral: false})

	result := sm.CleanStale(&NodeConfig{Ephemeral: true}, dir)
	assert.True(t, result)
}

func TestCleanStale_CleanupRemovesDatadir(t *testing.T) {
	t.Parallel()

	sm := newStateManager(t)
	dir := t.TempDir()
	touchStateFile(t, dir)
	writeMetaFile(t, dir, &stateMeta{Ephemeral: false})

	sm.CleanStale(&NodeConfig{Ephemeral: true}, dir)

	_, err := os.Stat(dir)
	assert.True(t, os.IsNotExist(err), "datadir should be removed after cleanup")
}

// --- Save ---

func TestSave_CreatesMetaFile(t *testing.T) {
	t.Parallel()

	sm := newStateManager(t)
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(dir, 0o750))

	sm.Save(&NodeConfig{Ephemeral: true}, dir)

	assert.FileExists(t, filepath.Join(dir, "tsdproxy.yaml"))
}

func TestSave_OverwritesExisting(t *testing.T) {
	t.Parallel()

	sm := newStateManager(t)
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(dir, 0o750))

	sm.Save(&NodeConfig{Ephemeral: false}, dir)
	sm.Save(&NodeConfig{Ephemeral: true}, dir)

	metaFile := filepath.Join(dir, "tsdproxy.yaml")
	data, err := os.ReadFile(metaFile)
	require.NoError(t, err)
	assert.Contains(t, string(data), "true")
}

func TestSave_CreatesDirIfMissing(t *testing.T) {
	t.Parallel()

	sm := newStateManager(t)
	parent := t.TempDir()
	dir := filepath.Join(parent, "nested", "deep")

	sm.Save(&NodeConfig{Ephemeral: false}, dir)

	assert.FileExists(t, filepath.Join(dir, "tsdproxy.yaml"))
}

// --- CleanAll ---

func TestCleanAll_RemovesDirectory(t *testing.T) {
	t.Parallel()

	sm := newStateManager(t)
	dir := t.TempDir()
	touchStateFile(t, dir)
	assert.FileExists(t, filepath.Join(dir, "tailscaled.state"))

	sm.CleanAll(dir)

	_, err := os.Stat(dir)
	assert.True(t, os.IsNotExist(err), "directory should be removed by CleanAll")
}

func TestCleanAll_DirNotExists_NoError(t *testing.T) {
	t.Parallel()

	sm := newStateManager(t)
	dir := filepath.Join(t.TempDir(), "nonexistent")

	sm.CleanAll(dir)
}

// --- CleanAuthState ---

func TestCleanAuthState_RemovesStateFiles(t *testing.T) {
	t.Parallel()

	sm := newStateManager(t)
	dir := t.TempDir()

	require.NoError(t, os.MkdirAll(dir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "tailscaled.state"), []byte("state"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "tailscaled.log1.txt"), []byte("log1"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "tailscaled.log2.txt"), []byte("log2"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "tailscaled.log.conf"), []byte("config"), 0o600))

	sm.CleanAuthState(dir)

	assert.NoFileExists(t, filepath.Join(dir, "tailscaled.state"))
	assert.NoFileExists(t, filepath.Join(dir, "tailscaled.log1.txt"))
	assert.NoFileExists(t, filepath.Join(dir, "tailscaled.log2.txt"))
	assert.NoFileExists(t, filepath.Join(dir, "tailscaled.log.conf"))
}

func TestCleanAuthState_NoFiles_NoError(t *testing.T) {
	t.Parallel()

	sm := newStateManager(t)
	dir := t.TempDir()

	sm.CleanAuthState(dir)
}

// --- CleanAuthStateDirs ---

func TestCleanAuthStateDirs_Multiple(t *testing.T) {
	t.Parallel()

	sm := newStateManager(t)
	dir1 := t.TempDir()
	dir2 := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(dir1, "tailscaled.state"), []byte("state"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir2, "tailscaled.state"), []byte("state"), 0o600))

	sm.CleanAuthStateDirs(dir1, dir2)

	assert.NoFileExists(t, filepath.Join(dir1, "tailscaled.state"))
	assert.NoFileExists(t, filepath.Join(dir2, "tailscaled.state"))
}

func TestCleanAuthStateDirs_EmptyString(t *testing.T) {
	t.Parallel()

	sm := newStateManager(t)
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "tailscaled.state"), []byte("state"), 0o600))

	sm.CleanAuthStateDirs(dir, "")
	assert.NoFileExists(t, filepath.Join(dir, "tailscaled.state"))
}

// --- Lifecycle ---

func TestLifecycle_FullCycle(t *testing.T) {
	t.Parallel()

	sm := newStateManager(t)
	dir := t.TempDir()

	assert.False(t, sm.StateExists(dir), "no state initially")

	touchStateFile(t, dir)
	assert.True(t, sm.StateExists(dir), "state file present")

	sm.Save(&NodeConfig{Ephemeral: true}, dir)

	assert.False(t, sm.CleanStale(&NodeConfig{Ephemeral: true}, dir), "same ephemeral, no cleanup")
	assert.True(t, sm.StateExists(dir), "state still present after same-ephemeral check")

	assert.True(t, sm.CleanStale(&NodeConfig{Ephemeral: false}, dir), "ephemeral changed, cleanup triggered")
	assert.False(t, sm.StateExists(dir), "state removed after cleanup")
}
