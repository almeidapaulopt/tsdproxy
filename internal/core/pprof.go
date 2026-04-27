// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package core

import (
	"net/http"
	"net/http/pprof"
	"os"
)

func PprofAddRoutes(srv *HTTPServer) {
	if os.Getenv("TSDPROXY_PPROF") != "true" {
		return
	}
	srv.Get("/debug/pprof/", http.HandlerFunc(pprof.Index))
	srv.Get("/debug/pprof/cmdline", http.HandlerFunc(pprof.Cmdline))
	srv.Get("/debug/pprof/profile", http.HandlerFunc(pprof.Profile))
	srv.Get("/debug/pprof/symbol", http.HandlerFunc(pprof.Symbol))
	srv.Get("/debug/pprof/trace", http.HandlerFunc(pprof.Trace))
}
