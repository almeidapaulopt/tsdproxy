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

	"github.com/almeidapaulopt/tsdproxy/internal/model"
	"github.com/rs/zerolog"
)

var safeUserID = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

func defaultPreferences() model.Preferences {
	return model.Preferences{
		Dark:         true,
		View:         "card",
		Sort:         "name",
		Grouped:      false,
		FilterStatus: "all",
		FilterHealth: "all",
		Pinned:       []string{},
	}
}

var validSortKeys = map[string]bool{
	"name": true, "status": true, "provider": true, "health": true,
}

var validViewValues = map[string]bool{
	"card": true, "compact": true,
}

var validFilterStatusValues = map[string]bool{
	"all": true, "Running": true, "Stopped": true, "Error": true, "Paused": true, "Authenticating": true,
}

var validFilterHealthValues = map[string]bool{
	"all": true, "healthy": true, "down": true, "unknown": true,
}

func validatePrefs(p *model.Preferences) {
	def := defaultPreferences()
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
	dir   string
	log   zerolog.Logger
	mu    sync.RWMutex
	cache map[string]model.Preferences
}

func NewPreferencesStore(dir string, log zerolog.Logger) (*PreferencesStore, error) {
	prefDir := filepath.Join(dir, "dashboard", "preferences")
	if err := os.MkdirAll(prefDir, 0o755); err != nil {
		return nil, fmt.Errorf("create preferences dir: %w", err)
	}
	return &PreferencesStore{
		dir:   prefDir,
		log:   log.With().Str("component", "preferences").Logger(),
		cache: make(map[string]model.Preferences),
	}, nil
}

func (s *PreferencesStore) path(userID string) string {
	if !safeUserID.MatchString(userID) {
		userID = "_invalid"
	}
	return filepath.Join(s.dir, userID+".json")
}

func (s *PreferencesStore) Load(userID string) (model.Preferences, error) {
	s.mu.RLock()
	if cached, ok := s.cache[userID]; ok {
		s.mu.RUnlock()
		return cached, nil
	}
	s.mu.RUnlock()

	p := defaultPreferences()

	data, err := os.ReadFile(s.path(userID))
	if err != nil {
		if os.IsNotExist(err) {
			s.mu.Lock()
			s.cache[userID] = p
			s.mu.Unlock()
			return p, nil
		}
		return p, fmt.Errorf("read preferences: %w", err)
	}

	if err := json.Unmarshal(data, &p); err != nil {
		s.log.Warn().Err(err).Str("user", userID).Msg("corrupt preferences file, using defaults")
		p = defaultPreferences()
	}

	validatePrefs(&p)

	s.mu.Lock()
	s.cache[userID] = p
	s.mu.Unlock()

	return p, nil
}

func (s *PreferencesStore) Save(userID string, prefs model.Preferences) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	validatePrefs(&prefs)

	data, err := json.Marshal(prefs)
	if err != nil {
		return fmt.Errorf("marshal preferences: %w", err)
	}

	dst := s.path(userID)
	tmp := dst + ".tmp"

	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("write temp preferences: %w", err)
	}

	if err := os.Rename(tmp, dst); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename preferences: %w", err)
	}

	s.cache[userID] = prefs

	return nil
}

func (s *PreferencesStore) TogglePin(userID, proxyName string) (model.Preferences, error) {
	prefs, err := s.Load(userID)
	if err != nil {
		return prefs, err
	}

	pinnedSet := make(map[string]bool, len(prefs.Pinned))
	for _, p := range prefs.Pinned {
		pinnedSet[p] = true
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
	prefs.Pinned = newPinned

	if err := s.Save(userID, prefs); err != nil {
		return prefs, err
	}

	return prefs, nil
}
