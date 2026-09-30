// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package core

import (
	"bufio"
	"bytes"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rs/zerolog"
)

func TestLogRecord_WriteHeader(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	lr := &LogRecord{ResponseWriter: rec, status: http.StatusOK}

	lr.WriteHeader(http.StatusNotFound)

	if lr.status != http.StatusNotFound {
		t.Errorf("LogRecord.WriteHeader() status = %d, want %d", lr.status, http.StatusNotFound)
	}
	if rec.Code != http.StatusNotFound {
		t.Errorf("underlying ResponseWriter code = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestLogRecord_Write(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	lr := &LogRecord{ResponseWriter: rec, status: http.StatusOK}

	data := []byte("hello")
	n, err := lr.Write(data)
	if err != nil {
		t.Fatalf("LogRecord.Write() error: %v", err)
	}
	if n != len(data) {
		t.Errorf("LogRecord.Write() returned %d, want %d", n, len(data))
	}
	if lr.status != http.StatusOK {
		t.Errorf("LogRecord default status = %d, want %d", lr.status, http.StatusOK)
	}
	if rec.Body.String() != "hello" {
		t.Errorf("LogRecord.Write() body = %q, want %q", rec.Body.String(), "hello")
	}
}

type mockHijacker struct {
	http.ResponseWriter
}

func (m *mockHijacker) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return nil, nil, nil
}

func TestLogRecord_Hijack_Success(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	lr := &LogRecord{
		ResponseWriter: &mockHijacker{ResponseWriter: rec},
		status:         http.StatusOK,
	}

	conn, rw, err := lr.Hijack()
	if err != nil {
		t.Fatalf("LogRecord.Hijack() error: %v", err)
	}
	if conn != nil {
		t.Error("LogRecord.Hijack() conn = non-nil, want nil (mock)")
	}
	if rw != nil {
		t.Error("LogRecord.Hijack() rw = non-nil, want nil (mock)")
	}
}

func TestLogRecord_Hijack_NotSupported(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	lr := &LogRecord{ResponseWriter: rec, status: http.StatusOK}

	_, _, err := lr.Hijack()
	if err == nil {
		t.Fatal("LogRecord.Hijack() expected error, got nil")
	}
	if !errors.Is(err, ErrHijackNotSupported) {
		t.Errorf("LogRecord.Hijack() error = %v, want ErrHijackNotSupported", err)
	}
}

type mockFlusher struct {
	http.ResponseWriter
	flushed bool
}

func (m *mockFlusher) Flush() {
	m.flushed = true
}

func TestLogRecord_Flush(t *testing.T) {
	t.Parallel()

	flusher := &mockFlusher{ResponseWriter: httptest.NewRecorder()}
	lr := &LogRecord{ResponseWriter: flusher, status: http.StatusOK}

	lr.Flush()

	if !flusher.flushed {
		t.Error("LogRecord.Flush() did not call underlying Flush()")
	}
}

func TestLogRecord_Flush_NotSupported(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	lr := &LogRecord{ResponseWriter: rec, status: http.StatusOK}

	lr.Flush()
}

func TestLogRecord_Unwrap(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	lr := &LogRecord{ResponseWriter: rec, status: http.StatusOK}

	unwrapped := lr.Unwrap()
	if unwrapped != rec {
		t.Error("LogRecord.Unwrap() did not return underlying ResponseWriter")
	}
}

func TestLoggerMiddleware_SuccessStatus(t *testing.T) {
	t.Parallel()

	handler := LoggerMiddleware(zerolog.Nop(), http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("LoggerMiddleware() status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestLoggerMiddleware_ErrorStatus(t *testing.T) {
	t.Parallel()

	handler := LoggerMiddleware(zerolog.Nop(), http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/fail", nil)
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("LoggerMiddleware() status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
}

func TestLoggerMiddleware_WithAccessLogWriter(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	handler := LoggerMiddleware(
		zerolog.Nop(),
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
		WithAccessLogWriter(&buf),
	)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/access-test", nil)
	handler.ServeHTTP(rec, req)

	line := buf.String()
	if !strings.Contains(line, "200") {
		t.Errorf("access log line = %q, want status 200", line)
	}
	if !strings.Contains(line, "GET") {
		t.Errorf("access log line = %q, want method GET", line)
	}
	if !strings.Contains(line, "/access-test") {
		t.Errorf("access log line = %q, want path /access-test", line)
	}
}
