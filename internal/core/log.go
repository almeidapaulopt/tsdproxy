// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package core

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"github.com/almeidapaulopt/tsdproxy/internal/config"
)

var ErrHijackNotSupported = errors.New("hijack not supported")

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

	if logLevel == zerolog.DebugLevel || logLevel == zerolog.TraceLevel {
		logger = logger.With().Caller().Logger()
	}

	zerolog.SetGlobalLevel(logLevel)
	logger.Info().Str("Log level", config.Config.Log.Level).Msg("Log Settings")

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
		return nil, nil, ErrHijackNotSupported
	}
	return h.Hijack()
}

func (r *LogRecord) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Unwrap exposes the underlying ResponseWriter so that
// [http.NewResponseController] can reach optional methods
// such as SetReadDeadline / SetWriteDeadline implemented
// by the http server's connection writer.
//
// Without this, wrappers between the server and a streaming
// handler (e.g. httputil.ReverseProxy for SSE) cannot
// access those methods.
func (r *LogRecord) Unwrap() http.ResponseWriter {
	return r.ResponseWriter
}

// LoggerMiddlewareOption configures LoggerMiddleware behavior.
type LoggerMiddlewareOption func(*loggerMiddlewareConfig)

type loggerMiddlewareConfig struct {
	accessLogWriter io.Writer
}

// WithAccessLogWriter writes a compact access log line to w for every request.
// The line contains only the path to avoid leaking tokens in query strings.
// The caller must NOT pass a typed-nil pointer (e.g. (*LogRingBuffer)(nil))
// as an io.Writer; guard the call with a nil check on the concrete pointer.
func WithAccessLogWriter(w io.Writer) LoggerMiddlewareOption {
	return func(c *loggerMiddlewareConfig) {
		c.accessLogWriter = w
	}
}

// LoggerMiddleware logs incoming HTTP requests and optionally writes to an access log.
func LoggerMiddleware(l zerolog.Logger, next http.Handler, opts ...LoggerMiddlewareOption) http.Handler {
	cfg := loggerMiddlewareConfig{}
	for _, o := range opts {
		o(&cfg)
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lw := &LogRecord{
			ResponseWriter: w,
			status:         http.StatusOK,
		}

		t0 := time.Now()
		next.ServeHTTP(lw, r)
		elapsed := time.Since(t0)

		msg := "request"
		var event *zerolog.Event
		if lw.status >= http.StatusBadRequest {
			event = l.Error()
			if lw.err != nil {
				event = event.Err(lw.err)
			}
			msg = "error"
		} else {
			event = l.Info()
		}
		event.
			Int("status", lw.status).
			Str("method", r.Method).
			Str("host", r.Host).
			Str("client", r.RemoteAddr).
			Str("url", r.URL.Path).
			Dur("elapsed", elapsed).
			Msg(msg)

		if cfg.accessLogWriter != nil {
			fmt.Fprintf(cfg.accessLogWriter, "%s %d %s %s\n",
				t0.Format(time.RFC3339),
				lw.status,
				r.Method,
				r.URL.Path,
			)
		}
	})
}
