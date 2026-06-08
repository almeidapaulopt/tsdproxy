// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package dashboard

import (
	"bytes"
	"errors"
	"net/http"
	"testing"
)

// mockFlusher tracks whether http.Flusher.Flush() was called.
type mockFlusher struct {
	flushed bool
}

func (m *mockFlusher) Flush() {
	m.flushed = true
}

// mockResponseWriter combines http.ResponseWriter with mockFlusher.
type mockResponseWriter struct {
	header  http.Header
	flusher *mockFlusher
	buf     bytes.Buffer
}

func newMockResponseWriter() *mockResponseWriter {
	return &mockResponseWriter{
		header:  make(http.Header),
		flusher: &mockFlusher{},
	}
}

func (m *mockResponseWriter) Header() http.Header         { return m.header }
func (m *mockResponseWriter) Write(p []byte) (int, error) { return m.buf.Write(p) }
func (m *mockResponseWriter) WriteHeader(_ int)           {}
func (m *mockResponseWriter) Flush()                      { m.flusher.Flush() }

// errorWriter fails all writes.
type errorWriter struct {
	header http.Header
}

func (e *errorWriter) Header() http.Header         { return e.header }
func (e *errorWriter) Write(_ []byte) (int, error) { return 0, errors.New("write failed") }
func (e *errorWriter) WriteHeader(_ int)           {}

func TestWriteSSE_SingleLine(t *testing.T) {
	t.Parallel()

	w := newMockResponseWriter()

	err := WriteSSE(w, "test", "hello")
	if err != nil {
		t.Fatalf("WriteSSE returned unexpected error: %v", err)
	}

	got := w.buf.String()
	want := "event: test\ndata: hello\n\n"
	if got != want {
		t.Errorf("unexpected output:\ngot:  %q\nwant: %q", got, want)
	}
}

func TestWriteSSE_MultiLineData(t *testing.T) {
	t.Parallel()

	w := newMockResponseWriter()

	err := WriteSSE(w, "test", "line1\nline2\nline3")
	if err != nil {
		t.Fatalf("WriteSSE returned unexpected error: %v", err)
	}

	got := w.buf.String()
	want := "event: test\ndata: line1\ndata: line2\ndata: line3\n\n"
	if got != want {
		t.Errorf("unexpected output:\ngot:  %q\nwant: %q", got, want)
	}
}

func TestWriteSSE_EmptyData(t *testing.T) {
	t.Parallel()

	w := newMockResponseWriter()

	err := WriteSSE(w, "test", "")
	if err != nil {
		t.Fatalf("WriteSSE returned unexpected error: %v", err)
	}

	got := w.buf.String()
	want := "event: test\ndata: \n\n"
	if got != want {
		t.Errorf("unexpected output:\ngot:  %q\nwant: %q", got, want)
	}
}

func TestWriteSSE_FlushCalled(t *testing.T) {
	t.Parallel()

	w := newMockResponseWriter()

	err := WriteSSE(w, "test", "hello")
	if err != nil {
		t.Fatalf("WriteSSE returned unexpected error: %v", err)
	}

	if !w.flusher.flushed {
		t.Error("expected Flush() to be called, but it was not")
	}
}

func TestWriteSSE_WriteError(t *testing.T) {
	t.Parallel()

	w := &errorWriter{header: make(http.Header)}

	err := WriteSSE(w, "test", "hello")
	if err == nil {
		t.Fatal("expected error when writer fails, got nil")
	}
}
