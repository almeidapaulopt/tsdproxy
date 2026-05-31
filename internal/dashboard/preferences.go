// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package dashboard

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sync"

	"github.com/rs/zerolog"

	"github.com/almeidapaulopt/tsdproxy/internal/model"
)

var safeUserID = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

func defaultPreferences() model.Preferences {
	return model.Preferences{
		Dark:         true,
		View:         "card",
		Sort:         "name",
		Grouped:      false,
		FilterStatus: filterAll,
		FilterHealth: filterAll,
		Pinned:       []string{},
	}
}

var validSortKeys = map[string]bool{
	"name": true, sortStatus: true, "provider": true, sortHealth: true,
}

var validViewValues = map[string]bool{
	"card": true, "list": true,
}

var validFilterStatusValues = map[string]bool{
	filterAll: true, "Running": true, "Stopped": true, "Error": true, "Paused": true, "Authenticating": true, "AwaitingApproval": true,
}

var validFilterHealthValues = map[string]bool{
	filterAll: true, "healthy": true, "down": true, healthUnknown: true,
}

func validatePrefs(p *model.Preferences) {
	def := defaultPreferences()
	// Migration: "compact" was renamed to "list"
	if p.View == "compact" {
		p.View = "list"
	}
	if !validViewValues[p.View] {
		p.View = def.View
	}
	if !validSortKeys[p.Sort] {
		p.Sort = def.Sort
	}
	if !validFilterStatusValues[p.FilterStatus] {
		p.FilterStatus = def.FilterStatus
	}
	if !validFilterHealthValues[p.FilterHealth] {
		p.FilterHealth = def.FilterHealth
	}
	seen := make(map[string]bool, len(p.Pinned))
	clean := p.Pinned[:0]
	for _, name := range p.Pinned {
		if name != "" && !seen[name] {
			seen[name] = true
			clean = append(clean, name)
		}
	}
	p.Pinned = clean
}

type PreferencesStore struct {
	log   zerolog.Logger
	cache map[string]model.Preferences
	dir   string
	mu    sync.RWMutex
}

func NewPreferencesStore(dir string, log zerolog.Logger) (*PreferencesStore, error) {
	prefDir := filepath.Join(dir, "dashboard", "preferences")
	if err := os.MkdirAll(prefDir, 0o700); err != nil { //nolint:mnd
		return nil, fmt.Errorf("create preferences dir: %w", err)
	}
	return &PreferencesStore{
		dir:   prefDir,
		log:   log.With().Str("component", "preferences").Logger(),
		cache: make(map[string]model.Preferences),
	}, nil
}

func (s *PreferencesStore) path(userID string) string {
	return filepath.Join(s.dir, normalizeUserID(userID)+".json")
}

func normalizeUserID(userID string) string {
	if !safeUserID.MatchString(userID) {
		return "_invalid"
	}
	return userID
}

func (s *PreferencesStore) Load(userID string) (model.Preferences, error) {
	key := normalizeUserID(userID)
	s.mu.RLock()
	if cached, ok := s.cache[key]; ok {
		s.mu.RUnlock()
		return cached, nil
	}
	s.mu.RUnlock()

	s.mu.Lock()
	defer s.mu.Unlock()

	// Double-check after acquiring write lock.
	if cached, ok := s.cache[key]; ok {
		return cached, nil
	}

	return s.loadFromDisk(userID)
}

// Caller must hold s.mu write lock.
func (s *PreferencesStore) loadFromDisk(userID string) (model.Preferences, error) {
	key := normalizeUserID(userID)
	p := defaultPreferences()

	data, err := os.ReadFile(s.path(userID))
	if err != nil {
		if os.IsNotExist(err) {
			s.cache[key] = p
			return p, nil
		}
		return p, fmt.Errorf("read preferences: %w", err)
	}

	if err := json.Unmarshal(data, &p); err != nil {
		s.log.Warn().Err(err).Str("user", key).Msg("corrupt preferences file, using defaults")
		p = defaultPreferences()
	}

	validatePrefs(&p)

	s.cache[key] = p

	return p, nil
}

func (s *PreferencesStore) Save(userID string, prefs model.Preferences) error {
	return s.save(userID, prefs)
}

// prepareSave validates, marshals, and writes the temp file — no lock held.
func (s *PreferencesStore) prepareSave(prefs model.Preferences) (string, error) {
	validatePrefs(&prefs)

	data, err := json.Marshal(prefs)
	if err != nil {
		return "", fmt.Errorf("marshal preferences: %w", err)
	}

	f, err := os.CreateTemp(s.dir, "*.tmp")
	if err != nil {
		return "", fmt.Errorf("create temp preferences: %w", err)
	}
	tmp := f.Name()

	if _, err := f.Write(data); err != nil {
		f.Close()
		_ = os.Remove(tmp)
		return "", fmt.Errorf("write temp preferences: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return "", fmt.Errorf("close temp preferences: %w", err)
	}
	if err := os.Chmod(tmp, 0o600); err != nil { //nolint:mnd
		_ = os.Remove(tmp)
		return "", fmt.Errorf("chmod temp preferences: %w", err)
	}

	return tmp, nil
}

// commitSave renames temp to final and updates the cache.
// Caller must hold s.mu write lock.
func (s *PreferencesStore) commitSave(userID, tmp string, prefs model.Preferences) error {
	dst := s.path(userID)

	if err := os.Rename(tmp, dst); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename preferences: %w", err)
	}

	s.cache[normalizeUserID(userID)] = prefs

	return nil
}

// save validates, writes temp file (unlocked), then commits under the write lock.
func (s *PreferencesStore) save(userID string, prefs model.Preferences) error {
	tmp, err := s.prepareSave(prefs)
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	return s.commitSave(userID, tmp, prefs)
}

// Caller must hold s.mu write lock.
func (s *PreferencesStore) saveLocked(userID string, prefs model.Preferences) error {
	tmp, err := s.prepareSave(prefs)
	if err != nil {
		return err
	}
	return s.commitSave(userID, tmp, prefs)
}

// Update atomically loads, mutates, and saves preferences for a user.
// The fn callback receives the current prefs and may mutate it in place;
// the result is validated and persisted under the store's write lock,
// preventing lost-update races with concurrent requests (e.g. TogglePin).
func (s *PreferencesStore) Update(userID string, fn func(*model.Preferences)) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	prefs, err := s.loadFromDisk(userID)
	if err != nil {
		return err
	}

	fn(&prefs)

	return s.saveLocked(userID, prefs)
}

func (s *PreferencesStore) TogglePin(userID, proxyName string) (model.Preferences, error) {
	var prefs model.Preferences
	err := s.Update(userID, func(p *model.Preferences) {
		pinnedSet := make(map[string]bool, len(p.Pinned))
		for _, name := range p.Pinned {
			pinnedSet[name] = true
		}

		if pinnedSet[proxyName] {
			delete(pinnedSet, proxyName)
		} else {
			pinnedSet[proxyName] = true
		}

		newPinned := make([]string, 0, len(pinnedSet))
		for name := range pinnedSet {
			newPinned = append(newPinned, name)
		}
		p.Pinned = newPinned
		prefs = *p
	})
	if err != nil {
		return defaultPreferences(), err
	}
	return prefs, nil
}
