// SPDX-FileCopyrightText: 2024 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT
package proxyconfig

import (
	"net/url"
)

type (
	// Config struct stores all the configuration for the proxy
	Config struct {
		Tailscale *Tailscale
		// Global
		TargetProvider string
		TargetID       string
		ProxyProvider  string
		TargetURL      *url.URL
		ProxyURL       *url.URL
		Hostname       string
		ProxyAccessLog bool
	}

	// Tailscale struct stores the configuration for tailscale ProxyProvider
	Tailscale struct {
		AuthKey      string
		ControlURL   string
		Ephemeral    bool `default:"true" validate:"boolean"`
		RunWebClient bool `default:"false" validate:"boolean"`
		Verbose      bool `default:"false" validate:"boolean"`
		Funnel       bool `default:"false" validate:"boolean"`
	}
)

const (

	// Default values to proxyconfig
	//
	ProxyAccessLog = true
	ProxyProvider  = ""

	// tailscale defaults
	TailscaleEphemeral    = true
	TailscaleRunWebClient = false
	TailscaleVerbose      = false
	TailscaleFunnel       = false
	TailscaleControlURL   = ""
)
