// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package dnsproviders

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRetry_SucceedsImmediately(t *testing.T) {
	err := Retry(context.Background(), func() error { return nil }, 3, time.Millisecond)
	assert.NoError(t, err)
}

func TestRetry_SucceedsOnSecondAttempt(t *testing.T) {
	calls := 0
	err := Retry(context.Background(), func() error {
		calls++
		if calls < 2 {
			return errors.New("fail")
		}
		return nil
	}, 3, time.Millisecond)

	require.NoError(t, err)
	assert.Equal(t, 2, calls)
}

func TestRetry_ExhaustsRetries(t *testing.T) {
	err := Retry(context.Background(), func() error {
		return errors.New("always fail")
	}, 2, time.Millisecond)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "max retries (2) exceeded")
	assert.Contains(t, err.Error(), "always fail")
}

func TestRetry_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := Retry(ctx, func() error { return errors.New("fail") }, 3, time.Millisecond)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestRetry_BackoffIsExponential(t *testing.T) {
	var timestamps []time.Time

	err := Retry(context.Background(), func() error {
		timestamps = append(timestamps, time.Now())
		if len(timestamps) < 3 {
			return errors.New("fail")
		}
		return nil
	}, 5, 10*time.Millisecond)

	require.NoError(t, err)
	require.Len(t, timestamps, 3)

	gap1 := timestamps[1].Sub(timestamps[0])
	gap2 := timestamps[2].Sub(timestamps[1])

	assert.GreaterOrEqual(t, gap1.Milliseconds(), int64(8), "first backoff should be ~10ms")
	assert.GreaterOrEqual(t, gap2.Milliseconds(), int64(16), "second backoff should be ~20ms")
}

func TestRetry_OverflowGuardClampsToMaxBackoff(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	err := Retry(ctx, func() error { return errors.New("fail") }, 1, 1<<60)

	assert.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestRetry_BackoffClampedAtMaxBackoff(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	start := time.Now()
	err := Retry(ctx, func() error { return errors.New("fail") }, 2, 20*time.Second)
	elapsed := time.Since(start)

	require.Error(t, err)
	assert.Less(t, elapsed, 10*time.Second)
}

func TestRetry_ZeroRetries(t *testing.T) {
	calls := 0
	err := Retry(context.Background(), func() error {
		calls++
		return errors.New("fail")
	}, 0, time.Millisecond)

	require.Error(t, err)
	assert.Equal(t, 1, calls)
	assert.Contains(t, err.Error(), "max retries (0) exceeded")
}
