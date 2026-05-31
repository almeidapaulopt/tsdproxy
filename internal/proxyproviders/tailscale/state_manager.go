// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package tailscale

import (
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/rs/zerolog"

	"github.com/almeidapaulopt/tsdproxy/internal/config"
)

// stateMeta tracks the configuration used to create the current tsnet state,
// so incompatible config changes can be detected and stale state cleaned up.
type stateMeta struct {
	Hostname   string   `yaml:"hostname"`
	ControlURL string   `yaml:"controlUrl"`
	Mode       string   `yaml:"mode,omitempty"`
	Tags       []string `yaml:"tags,omitempty"`
	Ephemeral  bool     `yaml:"ephemeral"`
}

// StateManager handles local tsnet state metadata: compatibility checking,
// cleanup of stale state, and persistence of current state.
type StateManager struct {
	log zerolog.Logger
}

// NewStateManager creates a new StateManager.
func NewStateManager(log zerolog.Logger) *StateManager {
	return &StateManager{log: log}
}

// StateExists returns true if a tsnet state file exists in the given datadir.
func (sm *StateManager) StateExists(datadir string) bool {
	info, err := os.Stat(filepath.Join(datadir, "tailscaled.state"))
	return err == nil && !info.IsDir()
}

// CleanStale removes tsnet state when configuration has changed in ways that
// make existing state incompatible. Returns true if state was cleaned.
func (sm *StateManager) CleanStale(cfg *NodeConfig, datadir string) bool {
	stateFile := filepath.Join(datadir, "tailscaled.state")
	info, err := os.Stat(stateFile)
	if err != nil || info.IsDir() {
		return false
	}

	cached := new(stateMeta)
	file := config.NewConfigFile(sm.log, path.Join(datadir, "tsdproxy.yaml"), cached)
	if err := file.Load(); err != nil {
		return false
	}

	if (cached.Hostname != "" && cached.Hostname != cfg.Hostname) ||
		(cached.ControlURL != "" && cached.ControlURL != cfg.ControlURL) ||
		(cached.Mode != "" && cached.Mode != cfg.Mode) ||
		(len(cached.Tags) > 0 && strings.Join(cached.Tags, ",") != strings.Join(cleanTags(cfg.Tags), ",")) ||
		cached.Ephemeral != cfg.Ephemeral {
		sm.log.Info().
			Str("previous_hostname", cached.Hostname).
			Str("current_hostname", cfg.Hostname).
			Str("previous_control_url", cached.ControlURL).
			Str("current_control_url", cfg.ControlURL).
			Str("previous_mode", cached.Mode).
			Str("current_mode", cfg.Mode).
			Strs("previous_tags", cached.Tags).
			Strs("current_tags", cleanTags(cfg.Tags)).
			Bool("previous_ephemeral", cached.Ephemeral).
			Bool("current_ephemeral", cfg.Ephemeral).
			Msg("node configuration changed, clearing stale tsnet state")

		if err := os.RemoveAll(datadir); err != nil {
			sm.log.Error().Err(err).Msg("failed to clear stale tsnet state")
			return false
		}

		return true
	}

	return false
}

// CleanAll removes all tsnet state files in the given datadir.
func (sm *StateManager) CleanAll(datadir string) {
	if err := os.RemoveAll(datadir); err != nil {
		sm.log.Error().Err(err).Msg("failed to clear tsnet state")
	}
}

// CleanAuthState removes tsnet authentication state while preserving cached
// TLS certificates. This avoids unnecessary ACME re-registrations (and hitting
// Let's Encrypt rate limits) when auth state goes stale but certs are still valid.
func (sm *StateManager) CleanAuthState(datadir string) {
	removed := []string{}
	for _, name := range []string{
		"tailscaled.state",
		"tailscaled.log1.txt",
		"tailscaled.log2.txt",
		"tailscaled.log.conf",
	} {
		p := filepath.Join(datadir, name)
		if err := os.Remove(p); err == nil {
			removed = append(removed, name)
		} else if !os.IsNotExist(err) {
			sm.log.Warn().Err(err).Str("file", name).Msg("failed to remove tsnet auth state file")
		}
	}
	if len(removed) > 0 {
		sm.log.Debug().Strs("removed", removed).Msg("cleaned tsnet auth state, certificates preserved")
	}
	if err := os.Remove(filepath.Join(datadir, "profile-data")); err != nil && !os.IsNotExist(err) {
		sm.log.Warn().Err(err).Msg("failed to remove profile-data")
	}
}

func (sm *StateManager) CleanAuthStateDirs(dirs ...string) {
	for _, dir := range dirs {
		if dir != "" {
			sm.CleanAuthState(dir)
		}
	}
}

// Save persists the current node configuration as state metadata.
func (sm *StateManager) Save(cfg *NodeConfig, datadir string) {
	meta := &stateMeta{
		Hostname:   cfg.Hostname,
		ControlURL: cfg.ControlURL,
		Mode:       cfg.Mode,
		Tags:       cleanTags(cfg.Tags),
		Ephemeral:  cfg.Ephemeral,
	}
	file := config.NewConfigFile(sm.log, path.Join(datadir, "tsdproxy.yaml"), meta)
	if err := file.Save(); err != nil {
		sm.log.Error().Err(err).Msg("failed to save state metadata")
	}
}
