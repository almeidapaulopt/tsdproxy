// SPDX-FileCopyrightText: 2024 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT
package core

import (
	"bytes"
	"encoding/json"
	"net/http"

	"github.com/rs/zerolog"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// Middleware type as before.
type Middleware func(http.Handler) http.Handler

// HTTPServer struct to hold our routes and middleware.
type HTTPServer struct {
	Log         zerolog.Logger
	Mux         *http.ServeMux
	middlewares []Middleware
}

// NewHTTPServer creates and returns a new App with an initialized ServeMux and middleware slice.
func NewHTTPServer(log zerolog.Logger) *HTTPServer {
	return &HTTPServer{
		Mux:         http.NewServeMux(),
		middlewares: []Middleware{},
		Log:         log,
	}
}

// Use adds middleware to the chain.
func (a *HTTPServer) Use(mw Middleware) {
	a.middlewares = append(a.middlewares, mw)
}

// Handle registers a handler for a specific route, applying all middleware.
func (a *HTTPServer) Handle(pattern string, handler http.Handler) {
	finalHandler := handler
	for i := len(a.middlewares) - 1; i >= 0; i-- {
		finalHandler = a.middlewares[i](finalHandler)
	}

	a.Mux.Handle(pattern, finalHandler)
}

// StartServer starts a custom http server.
func (a *HTTPServer) StartServer(s *http.Server) error {
	// set Logger the first middlewares
	s.Handler = LoggerMiddleware(a.Log, a.Mux)

	if s.TLSConfig != nil {
		// add logger middleware
		return s.ListenAndServeTLS("", "")
	}

	return s.ListenAndServe()
}

func (a *HTTPServer) JSONResponse(w http.ResponseWriter, _ *http.Request, result interface{}) {
	body, err := json.Marshal(result)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		a.Log.Error().Err(err).Msg("JSON marshal failed in JSONResponse")
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	_, err = w.Write(a.prettyJSON(body))
	if err != nil {
		a.Log.Error().Err(err).Msg("Write failed in JSONResponse")
	}
}

func (a *HTTPServer) JSONResponseCode(w http.ResponseWriter, _ *http.Request, result interface{}, responseCode int) {
	body, err := json.Marshal(result)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		a.Log.Error().Err(err).Msg("JSON marshal failed")
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(responseCode)
	_, err = w.Write(a.prettyJSON(body))
	if err != nil {
		a.Log.Error().Err(err).Msg("Write failed in JSONResponseCode")
	}
}

func (a *HTTPServer) ErrorResponse(w http.ResponseWriter, _ *http.Request, span trace.Span, returnError string, code int) {
	data := struct {
		Message string `json:"message"`
		Code    int    `json:"code"`
	}{
		Code:    code,
		Message: returnError,
	}

	span.SetStatus(codes.Error, returnError)

	body, err := json.Marshal(data)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		a.Log.Error().Err(err).Msg("JSON marshal failed")
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	_, err = w.Write(a.prettyJSON(body))
	if err != nil {
		a.Log.Error().Err(err).Msg("Write failed in ErrorResponse")
	}
}

func (a *HTTPServer) prettyJSON(b []byte) []byte {
	var out bytes.Buffer
	if err := json.Indent(&out, b, "", "  "); err != nil {
		a.Log.Err(err).Msg("prettyJSON failed")
	}
	return out.Bytes()
}
