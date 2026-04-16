// SPDX-FileCopyrightText: 2025 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

//go:build pprof

package core

import (
	"net/http"
	"net/http/pprof"
)

func PprofAddRoutes(http *HTTPServer) {
	http.Get("/debug/pprof/", pprofIndex())
	http.Get("/debug/pprof/cmdline", pprofCmdline())
	http.Get("/debug/pprof/profile", pprofProfile())
	http.Get("/debug/pprof/symbol", pprofSymbol())
	http.Get("/debug/pprof/trace", pprofTrace())
}

func pprofIndex() http.HandlerFunc {
	return pprof.Index
}

func pprofCmdline() http.HandlerFunc {
	return pprof.Cmdline
}

func pprofProfile() http.HandlerFunc {
	return pprof.Profile
}

func pprofSymbol() http.HandlerFunc {
	return pprof.Symbol
}

func pprofTrace() http.HandlerFunc {
	return pprof.Trace
}
