// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package core

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
