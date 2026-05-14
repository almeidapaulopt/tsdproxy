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

func SSEAppendHTML(w http.ResponseWriter, v any) error {
	html, err := renderHTML(v)
	if err != nil {
		return err
	}
	return WriteSSE(w, "proxy-append", html)
}

func SSEMergeHTML(w http.ResponseWriter, v any) error {
	html, err := renderHTML(v)
	if err != nil {
		return err
	}
	return WriteSSE(w, "proxy-merge", html)
}

func SSERemoveElement(w http.ResponseWriter, selector string) error {
	return WriteSSE(w, "proxy-remove", selector)
}

func SSEClearList(w http.ResponseWriter, selector string) error {
	return WriteSSE(w, "list-clear", selector)
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
