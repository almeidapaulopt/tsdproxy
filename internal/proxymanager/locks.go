// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package proxymanager

import (
	"fmt"
	"os"
	"runtime/debug"
	"sync"
)

// keyedLocks provides per-key mutexes with automatic cleanup.
// Unlike sync.Map[string]*sync.Mutex, keyedLocks never leaks entries:
// when the last holder releases a key, the entry is removed automatically.
// It also never breaks serialization: callers that loaded the same
// ref-counted mutex are guaranteed to serialize, even during cleanup.
//
// Lock returns an unlock closure that is safe to call multiple times
// (subsequent calls are no-ops via sync.Once). This eliminates the
// double-unlock class of bugs entirely. The closure also validates
// pointer identity against the current map entry, preventing stale
// unlocks from corrupting a new lock holder when an entry was deleted
// and recreated between the original Lock and a late Unlock.
type keyedLocks struct {
	locks map[string]*refCountedMutex
	mu    sync.Mutex
}

type refCountedMutex struct {
	mu   sync.Mutex
	refs int
}

func newKeyedLocks() *keyedLocks {
	return &keyedLocks{locks: make(map[string]*refCountedMutex)}
}

// Lock acquires the mutex for the given key and returns an unlock function.
// The returned function is safe to call multiple times; only the first
// call releases the lock. Callers must call it exactly once (typically
// via defer).
func (kl *keyedLocks) Lock(key string) func() {
	kl.mu.Lock()
	rc, ok := kl.locks[key]
	if !ok {
		rc = &refCountedMutex{}
		kl.locks[key] = rc
	}
	rc.refs++
	kl.mu.Unlock()

	rc.mu.Lock()

	var once sync.Once
	return func() {
		once.Do(func() { kl.unlock(key, rc) })
	}
}

// unlock releases the mutex for the given key and removes the entry
// when the last holder releases it.
//
// If the entry was deleted (refs reached 0) and a new Lock created a
// fresh entry, the pointer will not match — this is a stale unlock
// from a previous generation (only possible if the caller bypassed
// the sync.Once guard in the returned closure). The call logs to
// stderr with a stack trace and returns without corrupting the new
// entry.
func (kl *keyedLocks) unlock(key string, rc *refCountedMutex) {
	kl.mu.Lock()
	current, ok := kl.locks[key]
	if !ok || current != rc {
		kl.mu.Unlock()
		fmt.Fprintf(os.Stderr,
			"keyedLocks.unlock: stale unlock for key %q (entry was already released or replaced)\n--- stack ---\n%s",
			key, debug.Stack())
		return
	}
	rc.refs--
	if rc.refs == 0 {
		delete(kl.locks, key)
	}
	kl.mu.Unlock()

	rc.mu.Unlock()
}

// count returns the number of active keys (for testing).
func (kl *keyedLocks) count() int {
	kl.mu.Lock()
	defer kl.mu.Unlock()
	return len(kl.locks)
}
