// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package model

const (
	// Default values to proxyconfig
	//
	DefaultProxyAccessLog = true
	DefaultProxyProvider  = ""
	DefaultTLSValidate    = true

	// tailscale defaults
	DefaultTailscaleEphemeral    = false
	DefaultTailscaleRunWebClient = false
	DefaultTailscaleVerbose      = false
	DefaultTailscaleFunnel       = false
	DefaultTailscaleControlURL   = ""

	// Dashboard defauts
	DefaultDashboardVisible = true
	DefaultDashboardIcon    = "tsdproxy"
)
