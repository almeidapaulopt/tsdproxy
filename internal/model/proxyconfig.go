// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package model

import (
	"fmt"

	"github.com/creasty/defaults"
)

type (

	// Config struct stores all the configuration for the proxy
	Config struct {
		Ports          PortConfigList `validate:"dive"`
		TargetProvider string
		TargetID       string
		ProxyProvider  string
		Hostname       string
		Dashboard      Dashboard `validate:"dive"`
		Tailscale      Tailscale `validate:"dive"`
		ProxyAccessLog bool      `default:"true" validate:"boolean"`
	}

	// Tailscale struct stores the configuration for tailscale ProxyProvider
	Tailscale struct {
		Tags         string `yaml:"tags"`
		AuthKey      string `yaml:"authKey"`
		Ephemeral    bool   `default:"false" validate:"boolean" yaml:"ephemeral"`
		RunWebClient bool   `default:"false" validate:"boolean" yaml:"runWebClient"`
		Verbose      bool   `default:"false" validate:"boolean" yaml:"verbose"`
	}

	Dashboard struct {
		Label   string `validate:"string" yaml:"label"`
		Icon    string `default:"tsdproxy" validate:"string" yaml:"icon"`
		Visible bool   `default:"true" validate:"boolean" yaml:"visible"`
	}

	PortConfigList map[string]PortConfig
)

func NewConfig() (*Config, error) {
	config := new(Config)

	err := defaults.Set(config)
	if err != nil {
		return nil, fmt.Errorf("error loading defaults: %w", err)
	}

	return config, nil
}
