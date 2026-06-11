// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package core

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestSessionMiddleware_NoCookie_CreatesNewSession(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)

	handler := SessionMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	cookies := rec.Result().Cookies()
	var sessionCookie *http.Cookie
	for _, c := range cookies {
		if c.Name == "session_id" {
			sessionCookie = c
			break
		}
	}

	if sessionCookie == nil {
		t.Fatal("no session_id cookie set")
	}

	if _, err := uuid.Parse(sessionCookie.Value); err != nil {
		t.Errorf("cookie value %q is not a valid UUID: %v", sessionCookie.Value, err)
	}

	if sessionCookie.HttpOnly != true {
		t.Error("cookie HttpOnly = false, want true")
	}
	if sessionCookie.Secure != true {
		t.Error("cookie Secure = false, want true")
	}
	if sessionCookie.SameSite != http.SameSiteStrictMode {
		t.Errorf("cookie SameSite = %v, want %v", sessionCookie.SameSite, http.SameSiteStrictMode)
	}
	if sessionCookie.MaxAge != sessionMaxAge {
		t.Errorf("cookie MaxAge = %d, want %d", sessionCookie.MaxAge, sessionMaxAge)
	}
	if sessionCookie.Path != "/" {
		t.Errorf("cookie Path = %q, want %q", sessionCookie.Path, "/")
	}
}

func TestSessionMiddleware_ValidExistingCookie_ReusesSessionID(t *testing.T) {
	t.Parallel()

	existingID := uuid.New().String()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{
		Name:  "session_id",
		Value: existingID,
	})

	var capturedSessionID string
	handler := SessionMiddleware(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		capturedSessionID = r.Header.Get("X-Session-ID")
	}))
	handler.ServeHTTP(rec, req)

	if capturedSessionID != existingID {
		t.Errorf("X-Session-ID = %q, want %q", capturedSessionID, existingID)
	}

	cookies := rec.Result().Cookies()
	for _, c := range cookies {
		if c.Name == "session_id" {
			t.Errorf("unexpected Set-Cookie for session_id (value=%q) when valid cookie existed", c.Value)
		}
	}
}

func TestSessionMiddleware_InvalidCookieValue_GeneratesNewUUID(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{
		Name:  "session_id",
		Value: "not-a-valid-uuid",
	})

	handler := SessionMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	cookies := rec.Result().Cookies()
	var sessionCookie *http.Cookie
	for _, c := range cookies {
		if c.Name == "session_id" {
			sessionCookie = c
			break
		}
	}

	if sessionCookie == nil {
		t.Fatal("no session_id cookie set for invalid cookie value")
	}

	if _, err := uuid.Parse(sessionCookie.Value); err != nil {
		t.Errorf("new cookie value %q is not a valid UUID: %v", sessionCookie.Value, err)
	}

	if sessionCookie.Value == "not-a-valid-uuid" {
		t.Error("new cookie value should differ from invalid input")
	}
}

func TestSessionMiddleware_SetsXSessionIDHeader(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)

	var capturedSessionID string
	handler := SessionMiddleware(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		capturedSessionID = r.Header.Get("X-Session-ID")
	}))
	handler.ServeHTTP(rec, req)

	if capturedSessionID == "" {
		t.Fatal("X-Session-ID header not set on request")
	}

	if _, err := uuid.Parse(capturedSessionID); err != nil {
		t.Errorf("X-Session-ID %q is not a valid UUID: %v", capturedSessionID, err)
	}

	cookies := rec.Result().Cookies()
	cookieValue := ""
	for _, c := range cookies {
		if c.Name == "session_id" {
			cookieValue = c.Value
			break
		}
	}
	if capturedSessionID != cookieValue {
		t.Errorf("X-Session-ID = %q, cookie value = %q, want match", capturedSessionID, cookieValue)
	}
}

func TestSessionMiddleware_CallsNextHandler(t *testing.T) {
	t.Parallel()

	nextCalled := false
	handler := SessionMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusAccepted)
	}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	handler.ServeHTTP(rec, req)

	if !nextCalled {
		t.Fatal("next handler was not called")
	}

	if rec.Code != http.StatusAccepted {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusAccepted)
	}

	if rec.Body.String() != "" {
		t.Logf("response body: %q", rec.Body.String())
	}

	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodGet, "/", nil)
	handler2 := SessionMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("hello"))
	}))
	handler2.ServeHTTP(rec2, req2)

	if !strings.Contains(rec2.Body.String(), "hello") {
		t.Errorf("response body = %q, want to contain %q", rec2.Body.String(), "hello")
	}
}
