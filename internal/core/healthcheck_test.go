// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package core

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/rs/zerolog"
)

func TestNewHealthHandler(t *testing.T) {
	srv := NewHTTPServer(zerolog.Nop())
	h := NewHealthHandler(srv, zerolog.Nop())

	if h == nil {
		t.Fatal("NewHealthHandler() returned nil")
	}
	if h.ready != NotReady {
		t.Errorf("NewHealthHandler() ready = %d, want %d", h.ready, NotReady)
	}
}

func TestHealth_Ready_NotReady(t *testing.T) {
	srv := NewHTTPServer(zerolog.Nop())
	_ = NewHealthHandler(srv, zerolog.Nop())

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/health/ready/", nil)
	srv.Mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("Ready() status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}

	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("Ready() body unmarshal error: %v", err)
	}
	if body["status"] != "NOK" {
		t.Errorf("Ready() status body = %q, want %q", body["status"], "NOK")
	}
}

func TestHealth_Ready_AfterSetReady(t *testing.T) {
	srv := NewHTTPServer(zerolog.Nop())
	h := NewHealthHandler(srv, zerolog.Nop())

	h.SetReady()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/health/ready/", nil)
	srv.Mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Ready() after SetReady() status = %d, want %d", rec.Code, http.StatusOK)
	}

	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("Ready() body unmarshal error: %v", err)
	}
	if body["status"] != "OK" {
		t.Errorf("Ready() status body = %q, want %q", body["status"], "OK")
	}
}

func TestHealth_Ready_SetNotReady(t *testing.T) {
	srv := NewHTTPServer(zerolog.Nop())
	h := NewHealthHandler(srv, zerolog.Nop())

	h.SetReady()
	h.SetNotReady()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/health/ready/", nil)
	srv.Mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("Ready() after SetNotReady() status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}

	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("Ready() body unmarshal error: %v", err)
	}
	if body["status"] != "NOK" {
		t.Errorf("Ready() status body = %q, want %q", body["status"], "NOK")
	}
}

func TestHealth_AddRoutes(t *testing.T) {
	srv := NewHTTPServer(zerolog.Nop())
	_ = NewHealthHandler(srv, zerolog.Nop())

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/health/ready/", nil)
	srv.Mux.ServeHTTP(rec, req)

	if rec.Code == 0 {
		t.Error("AddRoutes() did not register /health/ready/ route")
	}
}

func TestHealth_ConcurrentSetReadySetNotReady(t *testing.T) {
	srv := NewHTTPServer(zerolog.Nop())
	h := NewHealthHandler(srv, zerolog.Nop())

	const goroutines = 100
	var wg sync.WaitGroup
	wg.Add(goroutines * 2)

	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			h.SetReady()
		}()
		go func() {
			defer wg.Done()
			h.SetNotReady()
		}()
	}
	wg.Wait()

	// Final state should be one of the two valid values
	val := atomic.LoadInt32(&h.ready)
	if val != Ready && val != NotReady {
		t.Fatalf("expected ready state to be %d or %d, got %d", Ready, NotReady, val)
	}
}

func TestHealth_RapidToggles(t *testing.T) {
	srv := NewHTTPServer(zerolog.Nop())
	h := NewHealthHandler(srv, zerolog.Nop())

	h.SetReady()
	h.SetNotReady()
	h.SetReady()
	h.SetNotReady()
	h.SetReady()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/health/ready/", nil)
	srv.Mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 after final SetReady(), got %d", rec.Code)
	}

	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body unmarshal error: %v", err)
	}
	if body["status"] != "OK" {
		t.Errorf("expected OK, got %q", body["status"])
	}
}

func TestHealth_ConcurrentReadsAndWrites(t *testing.T) {
	srv := NewHTTPServer(zerolog.Nop())
	h := NewHealthHandler(srv, zerolog.Nop())

	const iterations = 50
	var wg sync.WaitGroup
	wg.Add(iterations * 3)

	for i := 0; i < iterations; i++ {
		go func() {
			defer wg.Done()
			h.SetReady()
		}()
		go func() {
			defer wg.Done()
			h.SetNotReady()
		}()
		go func() {
			defer wg.Done()
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/health/ready/", nil)
			srv.Mux.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK && rec.Code != http.StatusServiceUnavailable {
				t.Errorf("unexpected status code %d", rec.Code)
			}
		}()
	}
	wg.Wait()
}

func TestHealth_SetReadyIdempotent(t *testing.T) {
	srv := NewHTTPServer(zerolog.Nop())
	h := NewHealthHandler(srv, zerolog.Nop())

	h.SetReady()
	h.SetReady()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/health/ready/", nil)
	srv.Mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 after double SetReady(), got %d", rec.Code)
	}
}

func TestHealth_SetNotReadyIdempotent(t *testing.T) {
	srv := NewHTTPServer(zerolog.Nop())
	h := NewHealthHandler(srv, zerolog.Nop())

	h.SetNotReady()
	h.SetNotReady()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/health/ready/", nil)
	srv.Mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503 after double SetNotReady(), got %d", rec.Code)
	}
}
