// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package core

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rs/zerolog"
)

func TestNewHTTPServer(t *testing.T) {
	t.Parallel()

	log := zerolog.Nop()
	srv := NewHTTPServer(log)

	if srv.Mux == nil {
		t.Fatal("NewHTTPServer() Mux is nil")
	}
	var buf bytes.Buffer
	testLog := zerolog.New(&buf)
	srvWithLog := NewHTTPServer(testLog)
	srvWithLog.Log.Info().Msg("test")
	if buf.Len() == 0 {
		t.Error("NewHTTPServer() Log was not assigned correctly")
	}
	if len(srv.middlewares) != 0 {
		t.Errorf("NewHTTPServer() middlewares = %d, want 0", len(srv.middlewares))
	}
}

func TestHTTPServer_Use(t *testing.T) {
	t.Parallel()

	srv := NewHTTPServer(zerolog.Nop())

	called := false
	srv.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = true
			next.ServeHTTP(w, r)
		})
	})

	srv.Handle("/test", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	srv.Mux.ServeHTTP(rec, req)

	if !called {
		t.Error("Use() middleware was not called")
	}
}

func TestHTTPServer_Handle(t *testing.T) {
	t.Parallel()

	srv := NewHTTPServer(zerolog.Nop())

	srv.Handle("/test", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	srv.Mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Handle() status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestHTTPServer_Get(t *testing.T) {
	t.Parallel()

	srv := NewHTTPServer(zerolog.Nop())

	srv.Get("/test-get", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/test-get", nil)
	srv.Mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Get() status = %d, want %d", rec.Code, http.StatusOK)
	}

	postRec := httptest.NewRecorder()
	postReq := httptest.NewRequest(http.MethodPost, "/test-get", nil)
	srv.Mux.ServeHTTP(postRec, postReq)

	if postRec.Code == http.StatusOK {
		t.Error("Get() matched POST request, want method restriction")
	}
}

func TestHTTPServer_Post(t *testing.T) {
	t.Parallel()

	srv := NewHTTPServer(zerolog.Nop())

	srv.Post("/test-post", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/test-post", nil)
	srv.Mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Post() status = %d, want %d", rec.Code, http.StatusOK)
	}

	getRec := httptest.NewRecorder()
	getReq := httptest.NewRequest(http.MethodGet, "/test-post", nil)
	srv.Mux.ServeHTTP(getRec, getReq)

	if getRec.Code == http.StatusOK {
		t.Error("Post() matched GET request, want method restriction")
	}
}

func TestHTTPServer_Put(t *testing.T) {
	t.Parallel()

	srv := NewHTTPServer(zerolog.Nop())

	srv.Put("/test-put", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/test-put", nil)
	srv.Mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Put() status = %d, want %d", rec.Code, http.StatusOK)
	}

	getRec := httptest.NewRecorder()
	getReq := httptest.NewRequest(http.MethodGet, "/test-put", nil)
	srv.Mux.ServeHTTP(getRec, getReq)

	if getRec.Code == http.StatusOK {
		t.Error("Put() matched GET request, want method restriction")
	}
}

func TestHTTPServer_MiddlewareChain(t *testing.T) {
	t.Parallel()

	srv := NewHTTPServer(zerolog.Nop())

	var order []string

	srv.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			order = append(order, "first")
			next.ServeHTTP(w, r)
		})
	})

	srv.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			order = append(order, "second")
			next.ServeHTTP(w, r)
		})
	})

	srv.Handle("/chain", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		order = append(order, "handler")
		w.WriteHeader(http.StatusOK)
	}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/chain", nil)
	srv.Mux.ServeHTTP(rec, req)

	if len(order) != 3 {
		t.Fatalf("middleware chain length = %d, want 3", len(order))
	}
	if order[0] != "first" {
		t.Errorf("order[0] = %q, want %q", order[0], "first")
	}
	if order[1] != "second" {
		t.Errorf("order[1] = %q, want %q", order[1], "second")
	}
	if order[2] != "handler" {
		t.Errorf("order[2] = %q, want %q", order[2], "handler")
	}
}

func TestJSONResponse(t *testing.T) {
	t.Parallel()

	srv := NewHTTPServer(zerolog.Nop())

	data := map[string]string{"key": "value"}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)

	srv.JSONResponse(rec, req, data)

	if rec.Code != http.StatusOK {
		t.Errorf("JSONResponse() status = %d, want %d", rec.Code, http.StatusOK)
	}

	ct := rec.Header().Get("Content-Type")
	if !strings.Contains(ct, "application/json") {
		t.Errorf("JSONResponse() Content-Type = %q, want application/json", ct)
	}

	var result map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("JSONResponse() body unmarshal error: %v", err)
	}
	if result["key"] != "value" {
		t.Errorf("JSONResponse() body key = %q, want %q", result["key"], "value")
	}
}

func TestJSONResponseCode(t *testing.T) {
	t.Parallel()

	srv := NewHTTPServer(zerolog.Nop())

	data := map[string]string{"created": "true"}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)

	srv.JSONResponseCode(rec, req, data, http.StatusCreated)

	if rec.Code != http.StatusCreated {
		t.Errorf("JSONResponseCode() status = %d, want %d", rec.Code, http.StatusCreated)
	}

	ct := rec.Header().Get("Content-Type")
	if !strings.Contains(ct, "application/json") {
		t.Errorf("JSONResponseCode() Content-Type = %q, want application/json", ct)
	}

	var result map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("JSONResponseCode() body unmarshal error: %v", err)
	}
	if result["created"] != "true" {
		t.Errorf("JSONResponseCode() body created = %q, want %q", result["created"], "true")
	}
}

func TestErrorResponse(t *testing.T) {
	t.Parallel()

	srv := NewHTTPServer(zerolog.Nop())

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)

	srv.ErrorResponse(rec, req, "something went wrong", http.StatusBadRequest)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("ErrorResponse() status = %d, want %d", rec.Code, http.StatusBadRequest)
	}

	ct := rec.Header().Get("Content-Type")
	if !strings.Contains(ct, "application/json") {
		t.Errorf("ErrorResponse() Content-Type = %q, want application/json", ct)
	}

	var result map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("ErrorResponse() body unmarshal error: %v", err)
	}
	if msg, _ := result["message"].(string); msg != "something went wrong" {
		t.Errorf("ErrorResponse() message = %q, want %q", msg, "something went wrong")
	}
	if code, _ := result["code"].(float64); int(code) != http.StatusBadRequest {
		t.Errorf("ErrorResponse() code = %v, want %d", code, http.StatusBadRequest)
	}
}

func TestErrorResponse_InternalServerError(t *testing.T) {
	t.Parallel()

	srv := NewHTTPServer(zerolog.Nop())

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)

	srv.ErrorResponse(rec, req, "test error", http.StatusInternalServerError)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("ErrorResponse() status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
}

func TestPrettyJSON(t *testing.T) {
	t.Parallel()

	srv := NewHTTPServer(zerolog.Nop())

	input := map[string]string{"hello": "world"}
	raw, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("json.Marshal error: %v", err)
	}

	pretty := srv.prettyJSON(raw)

	if !strings.Contains(string(pretty), "  ") {
		t.Errorf("prettyJSON() output = %q, want indented JSON", string(pretty))
	}

	var result map[string]string
	if err := json.Unmarshal(pretty, &result); err != nil {
		t.Fatalf("prettyJSON() output unmarshal error: %v", err)
	}
	if result["hello"] != "world" {
		t.Errorf("prettyJSON() output hello = %q, want %q", result["hello"], "world")
	}
}
