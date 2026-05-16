// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package proxymanager

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

func TestHealthChecker_FiresAfterThreeFailures(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
		t.Fatalf("onRedetect fired with cancelled context: %d", count)
	}
}

func TestHealthChecker_SetTargetUpdatesProbe(t *testing.T) {
	backendA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer backendA.Close()

	backendB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

func TestHealthChecker_AfterFireRequiresNewHealthyPeriod(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	backend2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
