// SPDX-FileCopyrightText: 2025 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package core

import (
	"bufio"
	"errors"
	"net"
	"net/http"
	"os"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"github.com/almeidapaulopt/tsdproxy/internal/config"
)

func NewLog() zerolog.Logger {
	println("Setting up logger")

	var logger zerolog.Logger

	if config.Config.Log.JSON {
		logger = zerolog.New(os.Stderr).With().Timestamp().Logger()
	} else {
		logger = zerolog.New(zerolog.ConsoleWriter{Out: os.Stdout}).With().Timestamp().Logger()
	}

	log.Logger = logger
	logLevel, err := zerolog.ParseLevel(config.Config.Log.Level)
	if err != nil {
		logger.Fatal().Err(err).Msg("Could not parse log level")
	}

	zerolog.SetGlobalLevel(logLevel)
	logger.Info().Str("Log level", config.Config.Log.Level).Msg("Log Settings")
	// l := &Logger{Logger: logger}

	return logger
}

// LogRecord warps a http.ResponseWriter and records the status.
type LogRecord struct {
	err error
	http.ResponseWriter
	status int
}

// WriteHeader overrides ResponseWriter.WriteHeader to keep track of the response code.
func (r *LogRecord) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func (r *LogRecord) Write(data []byte) (int, error) {
	n, err := r.ResponseWriter.Write(data)
	if err != nil {
		r.err = err
	}

	return n, err
}

func (r *LogRecord) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	h, ok := r.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, errors.New("hijack not supported")
	}
	return h.Hijack()
}

// LoggerMiddleware is a middleware function that logs incoming HTTP requests.
func LoggerMiddleware(l zerolog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lw := &LogRecord{
			ResponseWriter: w,
			status:         http.StatusOK,
		}

		// Call the next handler in the chain
		// lw := &loggingResponseWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(lw, r)
		// Log the request method and URL
		if lw.status >= http.StatusBadRequest {
			l.Error().
				Err(lw.err).
				Int("status", lw.status).
				Str("method", r.Method).
				Str("host", r.Host).
				Str("url", r.URL.String()).
				Msg("error")
		} else {
			l.Info().
				Int("status", lw.status).
				Str("method", r.Method).
				Str("host", r.Host).
				Str("url", r.URL.String()).
				Msg("request")
		}
	})
}
