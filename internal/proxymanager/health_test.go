// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package proxymanager

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
)

func TestHealthChecker_FiresAfterThreeFailures(t *testing.T) {
	t.Parallel()
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	var redetectCount int32
	hc := newHealthChecker(zerolog.Nop(), backend.URL, "http", 30*time.Second, 3, 0, true, func() error {
		atomic.AddInt32(&redetectCount, 1)
		return nil
	})
	defer hc.stop()

	for i := 0; i < 3; i++ {
		hc.check()
	}
	if count := atomic.LoadInt32(&redetectCount); count != 0 {
		t.Fatalf("onRedetect fired during healthy period: %d", count)
	}

	backend.Close()

	for i := 0; i < 3; i++ {
		hc.check()
	}
	if count := atomic.LoadInt32(&redetectCount); count != 1 {
		t.Fatalf("expected 1 onRedetect after 3 failures, got %d", count)
	}

	// Next checks are in backoff — should not fire again.
	for i := 0; i < 5; i++ {
		hc.check()
	}
	if count := atomic.LoadInt32(&redetectCount); count != 1 {
		t.Fatalf("expected onRedetect to stay at 1 during backoff, got %d", count)
	}
}

func TestHealthChecker_FiresEvenIfNeverHealthy(t *testing.T) {
	t.Parallel()
	var redetectCount int32
	hc := newHealthChecker(zerolog.Nop(), "http://127.0.0.1:1", "http", 30*time.Second, 3, 0, true, func() error {
		atomic.AddInt32(&redetectCount, 1)
		return nil
	})
	defer hc.stop()

	// Target is unreachable from the start — should still fire after 3 failures.
	for i := 0; i < 3; i++ {
		hc.check()
	}
	if count := atomic.LoadInt32(&redetectCount); count != 1 {
		t.Fatalf("expected onRedetect to fire for never-healthy target after 3 failures, got %d", count)
	}
}

func TestHealthChecker_CancelledContextNoFire(t *testing.T) {
	t.Parallel()
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	var redetectCount int32
	hc := newHealthChecker(zerolog.Nop(), backend.URL, "http", 30*time.Second, 3, 0, true, func() error {
		atomic.AddInt32(&redetectCount, 1)
		return nil
	})
	defer hc.stop()

	// Establish healthy state.
	hc.check()
	if count := atomic.LoadInt32(&redetectCount); count != 0 {
		t.Fatalf("unexpected onRedetect during healthy setup: %d", count)
	}

	// Cancel the context — simulates proxy shutdown.
	hc.cancel()

	// Kill the backend so checks fail.
	backend.Close()

	// Even after many failures, onRedetect must not fire because ctx.Err() != nil.
	for i := 0; i < 5; i++ {
		hc.check()
	}
	if count := atomic.LoadInt32(&redetectCount); count != 0 {
		t.Fatalf("onRedetect fired with canceled context: %d", count)
	}
}

func TestHealthChecker_SetTargetUpdatesProbe(t *testing.T) {
	t.Parallel()
	backendA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer backendA.Close()

	backendB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer backendB.Close()

	hc := newHealthChecker(zerolog.Nop(), backendA.URL, "http", 30*time.Second, 3, 0, true, func() error { return nil })
	defer hc.stop()

	// Healthy against backend A.
	hc.check()
	if r := hc.GetHealth(); r.Status != HealthHealthy {
		t.Fatalf("expected healthy on backend A, got %s", r.Status)
	}

	// Kill A — next check should be down.
	backendA.Close()
	hc.check()
	if r := hc.GetHealth(); r.Status != HealthDown {
		t.Fatalf("expected down after closing A, got %s", r.Status)
	}

	// Retarget to backend B.
	hc.SetTarget(backendB.URL)

	// Should be healthy again.
	hc.check()
	if r := hc.GetHealth(); r.Status != HealthHealthy {
		t.Fatalf("expected healthy on backend B after SetTarget, got %s", r.Status)
	}
}

func TestHealthChecker_CheckTCP_Healthy(t *testing.T) {
	t.Parallel()
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	target := backend.Listener.Addr().String()

	hc := newHealthChecker(zerolog.Nop(), target, "tcp", 30*time.Second, 3, 0, true, func() error {
		return nil
	})
	defer hc.stop()

	result := hc.checkTCP(context.Background())
	if result.Status != HealthHealthy {
		t.Fatalf("expected healthy, got %s (err: %s)", result.Status, result.Error)
	}
	if result.Latency <= 0 {
		t.Error("expected positive latency")
	}
}

func TestHealthChecker_CheckTCP_ConnectionRefused(t *testing.T) {
	t.Parallel()
	// Use a port that's very likely not listening
	hc := newHealthChecker(zerolog.Nop(), "tcp://127.0.0.1:1", "tcp", 30*time.Second, 3, 0, true, func() error {
		return nil
	})
	defer hc.stop()

	result := hc.checkTCP(context.Background())
	if result.Status != HealthDown {
		t.Fatalf("expected down, got %s", result.Status)
	}
}

func TestHealthChecker_CheckTCP_InvalidTarget(_ *testing.T) {
	hc := newHealthChecker(zerolog.Nop(), "tcp://[::1]:99999", "tcp", 30*time.Second, 3, 0, true, func() error {
		return nil
	})
	defer hc.stop()

	_ = hc.checkTCP(context.Background()) // should not panic
}

func TestHealthChecker_CheckUDP_ResolveError(t *testing.T) {
	t.Parallel()
	hc := newHealthChecker(zerolog.Nop(), "udp://[::1]:99999", "udp", 30*time.Second, 3, 0, true, func() error {
		return nil
	})
	defer hc.stop()

	result := hc.checkUDP(context.Background())
	if result.Status != HealthDown {
		t.Fatalf("expected down for invalid address, got %s", result.Status)
	}
}

func TestHealthChecker_CheckUDP_DialError(_ *testing.T) {
	hc := newHealthChecker(zerolog.Nop(), "udp://0.0.0.0:0", "udp", 30*time.Second, 3, 0, true, func() error {
		return nil
	})
	defer hc.stop()

	result := hc.checkUDP(context.Background())
	// dial to 0.0.0.0:0 may succeed but write may fail; either way should not panic
	_ = result
}

func TestHealthChecker_NextBackoff_OverflowProtection(t *testing.T) {
	t.Parallel()
	bo := nextBackoff(time.Hour, 63)
	if bo > maxBackoff {
		t.Fatalf("expected backoff capped at maxBackoff, got %v", bo)
	}
	if bo != maxBackoff {
		t.Fatalf("expected %v, got %v", maxBackoff, bo)
	}
}

func TestHealthChecker_NextBackoff_ZeroAttempt(t *testing.T) {
	t.Parallel()
	bo := nextBackoff(time.Minute, 0)
	if bo != time.Minute {
		t.Fatalf("expected 1 minute, got %v", bo)
	}
}

func TestHealthChecker_NextBackoff_Exponential(t *testing.T) {
	t.Parallel()
	// interval=1s, attempt=10 → 1024s
	bo := nextBackoff(time.Second, 10)
	expected := 1024 * time.Second // 2^10 = 1024
	if bo != expected {
		t.Fatalf("expected %v, got %v", expected, bo)
	}
}

func TestHealthChecker_GetHealth_NilReceiver(t *testing.T) {
	t.Parallel()
	var hc *healthChecker
	result := hc.GetHealth()
	if result.Status != HealthUnknown {
		t.Fatalf("expected HealthUnknown, got %s", result.Status)
	}
}

func TestHealthChecker_GetHealth_NilResult(t *testing.T) {
	t.Parallel()
	hc := &healthChecker{}
	result := hc.GetHealth()
	if result.Status != HealthUnknown {
		t.Fatalf("expected HealthUnknown for nil result, got %s", result.Status)
	}
}

func TestHealthChecker_Start_Run_Loop(t *testing.T) {
	t.Parallel()
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	hc := newHealthChecker(zerolog.Nop(), backend.URL, "http", 30*time.Millisecond, 3, 0, true, func() error {
		return nil
	})

	hc.start()

	require.Eventually(t, func() bool {
		return hc.GetHealth().Status == HealthHealthy
	}, 2*time.Second, 20*time.Millisecond)

	result := hc.GetHealth()
	hc.stop()

	if result.Status != HealthHealthy {
		t.Fatalf("expected healthy after run loop, got %s (err: %s)", result.Status, result.Error)
	}
}

func TestHealthChecker_String_Values(t *testing.T) {
	tests := []struct {
		want   string
		status HealthStatus
	}{
		{status: HealthHealthy, want: "healthy"},
		{status: HealthDown, want: "down"},
		{status: HealthUnknown, want: "unknown"},
		{status: HealthStatus(99), want: "unknown"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			t.Parallel()
			if got := tt.status.String(); got != tt.want {
				t.Errorf("HealthStatus(%d).String() = %q, want %q", tt.status, got, tt.want)
			}
		})
	}
}

func TestHealthChecker_AfterFireRequiresNewHealthyPeriod(t *testing.T) {
	t.Parallel()
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	var redetectCount int32
	hc := newHealthChecker(zerolog.Nop(), backend.URL, "http", 30*time.Second, 3, 0, true, func() error {
		atomic.AddInt32(&redetectCount, 1)
		return nil
	})
	defer hc.stop()

	// Phase 1: establish healthy state.
	for i := 0; i < 3; i++ {
		hc.check()
	}
	if count := atomic.LoadInt32(&redetectCount); count != 0 {
		t.Fatalf("phase 1: unexpected onRedetect: %d", count)
	}

	// Phase 2: kill backend, trigger 3 failures → onRedetect fires, counter reset.
	backend.Close()
	for i := 0; i < 3; i++ {
		hc.check()
	}
	if count := atomic.LoadInt32(&redetectCount); count != 1 {
		t.Fatalf("phase 2: expected 1 onRedetect, got %d", count)
	}

	// Phase 3: spin up a new healthy backend via SetTarget.
	backend2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer backend2.Close()

	hc.SetTarget(backend2.URL)

	// One healthy check re-establishes wasHealthy.
	hc.check()
	if r := hc.GetHealth(); r.Status != HealthHealthy {
		t.Fatalf("phase 3: expected healthy on new backend, got %s", r.Status)
	}

	// Phase 4: kill the new backend — 3 more failures should trigger onRedetect again.
	backend2.Close()
	for i := 0; i < 3; i++ {
		hc.check()
	}
	if count := atomic.LoadInt32(&redetectCount); count != 2 {
		t.Fatalf("phase 4: expected 2 total onRedetect (second cycle), got %d", count)
	}
}
