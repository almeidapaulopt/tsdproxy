// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package model

const (
	// Default values to proxyconfig
	//
	DefaultProxyAccessLog  = true
	DefaultIdentityHeaders = true
	DefaultProxyProvider   = ""
	DefaultTLSValidate     = true

	// tailscale defaults
	DefaultTailscaleEphemeral    = false
	DefaultTailscaleRunWebClient = false
	DefaultTailscaleVerbose      = false
	DefaultTailscaleFunnel       = false
	DefaultTailscaleControlURL   = ""

	// Max concurrent TLS cert generations against the Tailscale local API.
	// Prevents "context deadline exceeded" when many ephemeral containers
	// restart at once and all proxies request certs simultaneously.
	DefaultMaxCertConcurrency int64 = 2

	// Dashboard defaults
	DefaultDashboardVisible = true
	DefaultDashboardIcon    = "tsdproxy"

	// Rate limit defaults
	DefaultRateLimitEnabled = true
	DefaultRateLimitRPS     = 100
	DefaultRateLimitBurst   = 200

	// Rate limit bounds (min/max for label validation)
	RateLimitMinRPS   = 1
	RateLimitMaxRPS   = 10000
	RateLimitMinBurst = 1
	RateLimitMaxBurst = 100000
)

type Preferences struct {
	FilterHealth string   `json:"filterHealth"`
	FilterStatus string   `json:"filterStatus"`
	Sort         string   `json:"sort"`
	View         string   `json:"view"`
	Pinned       []string `json:"pinned"`
	Dark         bool     `json:"dark"`
	Grouped      bool     `json:"grouped"`
}
