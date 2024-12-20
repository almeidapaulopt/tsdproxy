// SPDX-FileCopyrightText: 2024 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT
package ui

import (
	"fmt"
	"net/http"

	"github.com/a-h/templ"
)

//go:generate templ generate

func Render(w http.ResponseWriter, r *http.Request, cmp templ.Component) error {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)

	err := cmp.Render(r.Context(), w)
	if err != nil {
		return fmt.Errorf("Failed to render template: %w", err)
	}

	return err
}
