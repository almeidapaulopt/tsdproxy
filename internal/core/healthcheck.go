// SPDX-FileCopyrightText: 2024 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package core

import (
	"net/http"
	"sync/atomic"

	"github.com/rs/zerolog"
)

const (
	Ready    = 1
	NotReady = 0
)

type Health struct {
	HTTP  *HTTPServer
	Log   zerolog.Logger
	ready int32
}

func NewHealthHandler(http *HTTPServer, log zerolog.Logger) *Health {
	h := &Health{
		HTTP: http,
		Log:  log,
	}

	atomic.StoreInt32(&h.ready, NotReady)

	h.AddRoutes()

	return h
}

func (h *Health) AddRoutes() {
	h.HTTP.Handle("GET /health/ready/", h.Ready())
}

func (h *Health) Ready() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if atomic.LoadInt32(&h.ready) == Ready {
			h.HTTP.JSONResponseCode(w, r, map[string]string{"status": "OK"}, http.StatusOK)
			return
		}

		h.HTTP.JSONResponseCode(w, r, map[string]string{"status": "NOK"}, http.StatusServiceUnavailable)
	}
}

func (h *Health) SetReady() {
	atomic.StoreInt32(&h.ready, Ready)
	h.Log.Info().Msgf("Health check set to ready")
}

func (h *Health) SetNotReady() {
	atomic.StoreInt32(&h.ready, NotReady)
	h.Log.Info().Msgf("Health check set to not ready")
}
