// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package ui

import (
	"bytes"
	"fmt"
	"net/http"

	"github.com/a-h/templ"
)

//go:generate templ generate

func RenderTempl(w http.ResponseWriter, r *http.Request, cmp templ.Component) error {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	var buf bytes.Buffer
	if err := cmp.Render(r.Context(), &buf); err != nil {
		return fmt.Errorf("failed to render template: %w", err)
	}

	w.WriteHeader(http.StatusOK)
	_, err := buf.WriteTo(w)
	return err
}
