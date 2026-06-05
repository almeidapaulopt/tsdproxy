// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testConfigData struct {
	Name    string `yaml:"name"`
	Port    int    `yaml:"port"`
	Enabled bool   `yaml:"enabled"`
}

func TestFile_Load_ValidYAML(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte("name: test\nport: 8080\nenabled: true\n"), 0o600))

	cfg := &testConfigData{}
	f := NewConfigFile(zerolog.Nop(), path, cfg)
	require.NoError(t, f.Load())

	assert.Equal(t, "test", cfg.Name)
	assert.Equal(t, 8080, cfg.Port)
	assert.True(t, cfg.Enabled)
}

func TestFile_Load_NonExistent(t *testing.T) {
	t.Parallel()

	f := NewConfigFile(zerolog.Nop(), "/nonexistent/path/config.yaml", &testConfigData{})
	err := f.Load()
	require.Error(t, err)
}

func TestFile_Load_EmptyFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte(""), 0o600))

	cfg := &testConfigData{}
	f := NewConfigFile(zerolog.Nop(), path, cfg)
	require.NoError(t, f.Load())
	assert.Equal(t, "", cfg.Name)
}

func TestFile_Load_InvalidYAML(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte("name: [unclosed"), 0o600))

	f := NewConfigFile(zerolog.Nop(), path, &testConfigData{})
	err := f.Load()
	require.Error(t, err)
}

func TestFile_Load_UnknownField(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte("name: test\nunknownField: bad\n"), 0o600))

	f := NewConfigFile(zerolog.Nop(), path, &testConfigData{})
	err := f.Load()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown field")
}

func TestFile_Load_DidYouMeanSuggestion(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	// "nme" is Levenshtein distance 1 from "name" — within the suggestion threshold.
	require.NoError(t, os.WriteFile(path, []byte("nme: test\n"), 0o600))

	f := NewConfigFile(zerolog.Nop(), path, &testConfigData{})
	err := f.Load()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown field")
	assert.Contains(t, err.Error(), "did you mean")
	assert.Contains(t, err.Error(), "name")
}

func TestFile_Load_MultipleUnknownFields(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte("name: test\nfoo: 1\nbar: 2\n"), 0o600))

	f := NewConfigFile(zerolog.Nop(), path, &testConfigData{})
	err := f.Load()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "foo")
	assert.Contains(t, err.Error(), "bar")
}

func TestFile_Save_NewFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	cfg := &testConfigData{Name: "test", Port: 9090, Enabled: true}
	f := NewConfigFile(zerolog.Nop(), path, cfg)
	require.NoError(t, f.Save())

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(data), "name: test")
	assert.Contains(t, string(data), "port: 9090")
}

func TestFile_Save_CreatesDirectory(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	nestedDir := filepath.Join(dir, "nested", "deep")
	path := filepath.Join(nestedDir, "config.yaml")

	f := NewConfigFile(zerolog.Nop(), path, &testConfigData{Name: "test"})
	require.NoError(t, f.Save())

	_, err := os.Stat(path)
	require.NoError(t, err)
}

func TestFile_Save_OverwriteExisting(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	f1 := NewConfigFile(zerolog.Nop(), path, &testConfigData{Name: "first", Port: 100})
	require.NoError(t, f1.Save())

	f2 := NewConfigFile(zerolog.Nop(), path, &testConfigData{Name: "second", Port: 200})
	require.NoError(t, f2.Save())

	cfg := &testConfigData{}
	f3 := NewConfigFile(zerolog.Nop(), path, cfg)
	require.NoError(t, f3.Load())
	assert.Equal(t, "second", cfg.Name)
	assert.Equal(t, 200, cfg.Port)
}

func TestFile_SaveLoad_Roundtrip(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	original := &testConfigData{Name: "roundtrip", Port: 443, Enabled: true}
	fSave := NewConfigFile(zerolog.Nop(), path, original)
	require.NoError(t, fSave.Save())

	loaded := &testConfigData{}
	fLoad := NewConfigFile(zerolog.Nop(), path, loaded)
	require.NoError(t, fLoad.Load())

	assert.Equal(t, original.Name, loaded.Name)
	assert.Equal(t, original.Port, loaded.Port)
	assert.Equal(t, original.Enabled, loaded.Enabled)
}

func TestFile_OnChange(t *testing.T) {
	t.Parallel()

	f := NewConfigFile(zerolog.Nop(), "test.yaml", &testConfigData{})

	called := false
	f.OnChange(func(_ fsnotify.Event) { called = true })

	// Verify callback was stored (no public getter, trigger via test event)
	f.mtx.Lock()
	cb := f.onChange
	f.mtx.Unlock()
	require.NotNil(t, cb)
	cb(fsnotify.Event{})
	assert.True(t, called)
}

func TestFile_OnChange_NilCallback(t *testing.T) {
	t.Parallel()

	f := NewConfigFile(zerolog.Nop(), "test.yaml", &testConfigData{})
	assert.NotPanics(t, func() { f.OnChange(nil) })
}

func TestFile_Watch_FileModifyTriggersCallback(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte("name: initial\n"), 0o600))

	f := NewConfigFile(zerolog.Nop(), path, &testConfigData{})

	triggered := make(chan fsnotify.Event, 1)
	f.OnChange(func(e fsnotify.Event) { triggered <- e })

	require.NoError(t, f.Watch())
	time.Sleep(200 * time.Millisecond)

	require.NoError(t, os.WriteFile(path, []byte("name: updated\n"), 0o600))

	select {
	case e := <-triggered:
		assert.True(t, e.Has(fsnotify.Write) || e.Has(fsnotify.Create))
	case <-time.After(5 * time.Second):
		t.Fatal("watch callback not triggered within 5s")
	}
}

func TestFile_Watch_NonExistentDirectory(t *testing.T) {
	f := NewConfigFile(zerolog.Nop(), "/nonexistent/dir/config.yaml", &testConfigData{})
	err := f.Watch()
	require.Error(t, err)
}
