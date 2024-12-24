// SPDX-FileCopyrightText: 2024 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT
package proxyconfig

import (
	"fmt"
	"net/url"

	"github.com/creasty/defaults"
)

type (
	// Config struct stores all the configuration for the proxy
	Config struct {
		Tailscale Tailscale `validate:"dive"`
		Dashboard Dashboard `validate:"dive"`
		// Global
		TargetProvider string
		TargetID       string
		ProxyProvider  string
		TargetURL      *url.URL
		ProxyURL       *url.URL
		Hostname       string
		ProxyAccessLog bool `default:"true" validate:"boolean"`
		TLSValidate    bool `default:"true" validate:"boolean"`
	}

	// Tailscale struct stores the configuration for tailscale ProxyProvider
	Tailscale struct {
		AuthKey      string `yaml:"authKey"`
		ControlURL   string `yaml:"controlUrl"`
		Ephemeral    bool   `default:"true" validate:"boolean" yaml:"ephemeral"`
		RunWebClient bool   `default:"false" validate:"boolean" yaml:"runWebClient"`
		Verbose      bool   `default:"false" validate:"boolean" yaml:"verbose"`
		Funnel       bool   `default:"false" validate:"boolean" yaml:"funnel"`
	}

	Dashboard struct {
		Visible bool `default:"true" validate:"boolean" yaml:"visible"`
	}
)

func NewConfig() (*Config, error) {
	config := new(Config)

	err := defaults.Set(config)
	if err != nil {
		return nil, fmt.Errorf("Error loading defaults: %w", err)
	}

	return config, nil
}

const (

	// Default values to proxyconfig
	//
	DefaultProxyAccessLog = true
	DefaultProxyProvider  = ""
	DefaultTLSValidate    = true

	// tailscale defaults
	DefaultTailscaleEphemeral    = true
	DefaultTailscaleRunWebClient = false
	DefaultTailscaleVerbose      = false
	DefaultTailscaleFunnel       = false
	DefaultTailscaleControlURL   = ""
)
