// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package lifecycle

import "sync"

// StateTracker tracks the status of per-domain operations with thread safety.
type StateTracker struct {
	states map[string]Status
	mu     sync.RWMutex
}

// NewStateTracker creates a new StateTracker.
func NewStateTracker() *StateTracker {
	return &StateTracker{
		states: make(map[string]Status),
	}
}

// Set updates the status for a domain.
func (t *StateTracker) Set(domain string, status Status) {
	t.mu.Lock()
	t.states[domain] = status
	t.mu.Unlock()
}

// Get returns the status for a domain.
func (t *StateTracker) Get(domain string) Status {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.states[domain]
}

// Delete removes the status entry for a domain, preventing unbounded map growth.
func (t *StateTracker) Delete(domain string) {
	t.mu.Lock()
	delete(t.states, domain)
	t.mu.Unlock()
}
