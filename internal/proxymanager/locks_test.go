// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package proxymanager

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestKeyedLocks_LockUnlockSucceeds(t *testing.T) {
	t.Parallel()

	kl := newKeyedLocks()
	const key = "ok-key"

	unlock := kl.Lock(key)
	require.NotPanics(t, func() {
		unlock()
	}, "single Lock/Unlock pair must not panic")

	require.Equal(t, 0, kl.count(), "keyed lock entry must be cleaned up after release")
}

func TestKeyedLocks_DoubleUnlockIsSafe(t *testing.T) {
	t.Parallel()

	kl := newKeyedLocks()
	const key = "double-unlock-key"

	unlock := kl.Lock(key)
	unlock()

	require.NotPanics(t, func() {
		unlock()
		unlock()
	}, "double-unlock via returned closure must be a safe no-op (sync.Once)")

	require.Equal(t, 0, kl.count(), "entry must be cleaned up after single effective unlock")
}

func TestKeyedLocks_ConcurrentSameKeySerializes(t *testing.T) {
	t.Parallel()

	kl := newKeyedLocks()
	const key = "concurrent-key"

	var order []int
	var mu sync.Mutex

	var wg sync.WaitGroup
	for i := range 3 {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			unlock := kl.Lock(key)
			defer unlock()

			mu.Lock()
			order = append(order, idx)
			mu.Unlock()
		}(i)
	}
	wg.Wait()

	require.Len(t, order, 3, "all goroutines must acquire the lock")
}

func TestKeyedLocks_StaleUnlockDoesNotCorruptNewHolder(t *testing.T) {
	t.Parallel()

	kl := newKeyedLocks()
	const key = "stale-key"

	unlock1 := kl.Lock(key)
	unlock1()

	unlock2 := kl.Lock(key)

	require.NotPanics(t, func() {
		unlock1()
	}, "stale unlock must not panic")

	count := kl.count()
	require.Equal(t, 1, count, "entry must still exist for the current holder after stale unlock")

	unlock2()
	require.Equal(t, 0, kl.count(), "entry must be cleaned up after current holder releases")
}
