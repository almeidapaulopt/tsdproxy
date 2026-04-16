// SPDX-FileCopyrightText: 2025 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

//go:build !pprof

package core

// PprofAddRoutes is a no-op unless the binary is built with the "pprof"
// build tag. This prevents /debug/pprof/* from being exposed to the
// tailnet (or to localhost) on default builds, where there is no
// authentication in front of it.
func PprofAddRoutes(_ *HTTPServer) {}
