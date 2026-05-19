// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package dashboard

//go:generate templ generate

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/a-h/templ"
)

func WriteSSE(w http.ResponseWriter, event string, data string) error {
	if _, err := fmt.Fprintf(w, "event: %s\n", event); err != nil {
		return fmt.Errorf("write sse event: %w", err)
	}
	for _, line := range strings.Split(data, "\n") {
		if _, err := fmt.Fprintf(w, "data: %s\n", line); err != nil { //nolint:gosec // G705: data is templ-rendered
			return fmt.Errorf("write sse data: %w", err)
		}
	}
	if _, err := fmt.Fprint(w, "\n"); err != nil {
		return fmt.Errorf("write sse delimiter: %w", err)
	}
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
	return nil
}

// WriteSSEPartialComponent renders a templ component inside an <hx-partial>
// element and sends it as an SSE data message. Pass nil cmp for swap-only
// operations (e.g. delete, outerHTML clear).
func WriteSSEPartialComponent(w http.ResponseWriter, target string, swap string, cmp templ.Component) error {
	var buf bytes.Buffer
	if err := ssePartialElement(target, swap, cmp).Render(context.Background(), &buf); err != nil {
		return fmt.Errorf("render sse partial: %w", err)
	}
	for _, line := range strings.Split(buf.String(), "\n") {
		if _, err := fmt.Fprintf(w, "data: %s\n", line); err != nil { //nolint:gosec // G705: line is templ-rendered
			return fmt.Errorf("write sse partial: %w", err)
		}
	}
	if _, err := fmt.Fprint(w, "\n"); err != nil {
		return fmt.Errorf("write sse delimiter: %w", err)
	}
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
	return nil
}
