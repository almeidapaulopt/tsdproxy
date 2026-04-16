// SPDX-FileCopyrightText: 2025 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package core

import (
	"errors"
	"net/http"
	"sync"

	"github.com/google/uuid"
)

// Session store (maps sessionID -> data)
var (
	sessions = make(map[string]map[string]string)
	mtx      sync.Mutex
)

// Middleware to manage sessions
func SessionMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check for existing session cookie
		cookie, err := r.Cookie("session_id")
		var sessionID string

		if errors.Is(err, http.ErrNoCookie) {
			// No session, create a new one
			sessionID = uuid.New().String()
			http.SetCookie(w, &http.Cookie{
				Name:     "session_id",
				Value:    sessionID,
				Path:     "/",
				HttpOnly: true,
				Secure:   true,
			})

			mtx.Lock()
			sessions[sessionID] = make(map[string]string)
			mtx.Unlock()
		} else {
			sessionID = cookie.Value
		}

		r.Header.Set("X-Session-ID", sessionID)
		next.ServeHTTP(w, r)
	})
}
