// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package core

import (
	"errors"
	"net/http"

	"github.com/google/uuid"
)

func SessionMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("session_id")
		var sessionID string

		if errors.Is(err, http.ErrNoCookie) {
			sessionID = uuid.New().String()
			http.SetCookie(w, &http.Cookie{ //nolint:gosec // G124: Secure is conditional on TLS
				Name:     "session_id",
				Value:    sessionID,
				Path:     "/",
				HttpOnly: true,
				Secure:   r.TLS != nil,
				SameSite: http.SameSiteStrictMode,
			})
		} else {
			sessionID = cookie.Value
		}

		r.Header.Set("X-Session-ID", sessionID)
		next.ServeHTTP(w, r)
	})
}
