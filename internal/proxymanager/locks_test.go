// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package proxymanager

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestKeyedLocks_DoubleUnlock_Panics_BUG reproduces the production-panic
// hazard at locks.go:54. A logic error anywhere in the call chain (e.g. a
// future refactor of eventStop or closeAndRemoveProxy) double-unlocks the
// same key, which currently calls panic() and tears down the whole tsdproxy
// process — taking every other proxy offline with it.
//
// For a long-running reverse proxy that may host hundreds of containers,
// a single caller bug should not crash the entire process. Acceptable
// post-fix behaviors:
//   - Return an error (preferred) so callers can log/recover.
//   - Log at Error level with runtime/debug.Stack() and continue.
//   - At minimum, recover() at the HandleProxyEvent boundary so an
//     in-flight event doesn't take down the whole process.
//
// Today: this test fails because Unlock panics instead of returning an error.
// The test catches the panic (via require.Panics) and asserts that the panic
// value is the expected one — then FAILS the assertion that the API should
// NOT panic in the first place.
//
// To "invert" once fixed: replace `require.Panics` with `require.NotPanics`
// and assert that the returned error is non-nil.
func TestKeyedLocks_DoubleUnlock_Panics_BUG(t *testing.T) {
	t.Parallel()

	kl := newKeyedLocks()
	const key = "test-target-id"

	kl.Lock(key)
	kl.Unlock(key)

	// BUG: today this panics. For a long-running proxy server, a panic in
	// an event-handling goroutine (which calls targetLocks.Unlock) would
	// crash the entire process unless every call site adds recover().
	//
	// After fix: should NOT panic. The function should either return an
	// error (signature change) or log + no-op.
	require.NotPanics(t, func() {
		// Today: panic("keyedLocks.Unlock: key \"test-target-id\" is not locked ...")
		// After fix: returns error or logs.
		defer func() {
			// Allow the panic to surface during the bug-confirmation phase
			// by re-panicking if no fix is present. require.NotPanics above
			// will then fail the test with a clear message.
			if r := recover(); r != nil {
				panic(r) // re-panic so NotPanics catches it
			}
		}()
		kl.Unlock(key)
	}, "BUG: keyedLocks.Unlock panics on double-unlock (locks.go:54) — "+
		"a process-killing panic is inappropriate for a long-running reverse proxy; "+
		"prefer returning an error or recovering at the HandleProxyEvent boundary")
}

// TestKeyedLocks_SingleUnlockSucceeds is a companion sanity test that
// documents the expected post-fix behavior: a single Lock/Unlock pair must
// continue to work without error or panic. Today this passes; it should
// continue to pass after the bug is fixed.
func TestKeyedLocks_SingleUnlockSucceeds(t *testing.T) {
	t.Parallel()

	kl := newKeyedLocks()
	const key = "ok-key"

	kl.Lock(key)
	// Single unlock must not panic.
	require.NotPanics(t, func() {
		kl.Unlock(key)
	}, "single Lock/Unlock pair must not panic")

	// After unlock, the entry should be cleaned up.
	require.Equal(t, 0, kl.count(), "keyed lock entry must be cleaned up after release")
}
