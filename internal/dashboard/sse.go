// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package dashboard

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
		if _, err := fmt.Fprintf(w, "data: %s\n", line); err != nil {
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

// WriteSSEPartial sends an unnamed SSE data message containing an <hx-partial>
// element for htmx 4 to process.
func WriteSSEPartial(w http.ResponseWriter, target string, swap string, html string) error {
	msg := fmt.Sprintf(
		`<hx-partial hx-target="%s" hx-swap="%s">%s</hx-partial>`,
		target, swap, html,
	)
	for _, line := range strings.Split(msg, "\n") {
		if _, err := fmt.Fprintf(w, "data: %s\n", line); err != nil {
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

// WriteSSEPartialComponent renders a templ component and sends it as an hx-partial.
func WriteSSEPartialComponent(w http.ResponseWriter, target string, swap string, cmp templ.Component) error {
	html, err := renderHTML(cmp)
	if err != nil {
		return err
	}
	return WriteSSEPartial(w, target, swap, html)
}

func SSEUpdateState(w http.ResponseWriter, jsonString string) error {
	return WriteSSE(w, "update-state", jsonString)
}

func renderHTML(v any) (string, error) {
	switch val := v.(type) {
	case string:
		return val, nil
	case templ.Component:
		var buf bytes.Buffer
		if err := val.Render(context.Background(), &buf); err != nil {
			return "", fmt.Errorf("render template: %w", err)
		}
		return buf.String(), nil
	default:
		return fmt.Sprintf("%v", val), nil
	}
}
