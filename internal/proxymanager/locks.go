// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package proxymanager

import (
	"fmt"
	"sync"
)

// keyedLocks provides per-key mutexes with automatic cleanup.
// Unlike sync.Map[string]*sync.Mutex, keyedLocks never leaks entries:
// when the last holder releases a key, the entry is removed automatically.
// It also never breaks serialization: callers that loaded the same
// ref-counted mutex are guaranteed to serialize, even during cleanup.
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

// Lock acquires the mutex for the given key. Every Lock must be paired
// with an Unlock for the same key from the same goroutine.
func (kl *keyedLocks) Lock(key string) {
	kl.mu.Lock()
	rc, ok := kl.locks[key]
	if !ok {
		rc = &refCountedMutex{}
		kl.locks[key] = rc
	}
	rc.refs++
	kl.mu.Unlock()

	rc.mu.Lock()
}

// Unlock releases the mutex for the given key and removes the entry
// when the last holder releases it.
// Panics with a descriptive message if the key was never locked or was
// already fully released, indicating a caller logic error (e.g. double-unlock).
func (kl *keyedLocks) Unlock(key string) {
	kl.mu.Lock()
	rc, ok := kl.locks[key]
	if !ok {
		kl.mu.Unlock()
		panic(fmt.Sprintf("keyedLocks.Unlock: key %q is not locked (double-unlock or unlocked-without-lock)", key))
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
