// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package model

import (
	"fmt"

	"github.com/creasty/defaults"

	"github.com/almeidapaulopt/tsdproxy/internal/core/secretstring"
)

type (

	// Config struct stores all the configuration for the proxy
	Config struct {
		Ports               PortConfigList `validate:"dive"`
		TargetProvider      string
		TargetID            string
		TargetImage         string
		ProxyProvider       string
		Hostname            string
		Domain              string    `validate:"omitempty,fqdn" yaml:"domain"`
		DNSProvider         string    `validate:"omitempty" yaml:"dnsProvider"`
		TLSProvider         string    `validate:"omitempty" yaml:"tlsProvider"`
		Dashboard           Dashboard `validate:"dive"`
		Tailscale           Tailscale `validate:"dive"`
		ProxyAccessLog      bool      `default:"true" validate:"boolean"`
		IdentityHeaders     bool      `default:"true" validate:"boolean"`
		AutoRestart         bool      `default:"true" validate:"boolean"`
		HealthCheckEnabled  bool      `default:"true" validate:"boolean"`
		HealthCheckInterval int       `default:"30" validate:"numeric,min=1"`
		HealthCheckFailures int       `default:"3" validate:"numeric,min=1"`
		HealthCheckCooldown int       `default:"0" validate:"numeric,min=0"`
	}

	// Tailscale struct stores the configuration for tailscale ProxyProvider
	Tailscale struct {
		Tags            string                    `yaml:"tags"`
		AuthKey         secretstring.SecretString `yaml:"authKey"`
		ResolvedAuthKey string                    `yaml:"-"` // pre-resolved by ResolveAuthKey, skips OAuth in getAuthkey
		Ephemeral       bool                      `default:"false" validate:"boolean" yaml:"ephemeral"`
		RunWebClient    bool                      `default:"false" validate:"boolean" yaml:"runWebClient"`
		Verbose         bool                      `default:"false" validate:"boolean" yaml:"verbose"`
	}

	Dashboard struct {
		Label    string `validate:"string" yaml:"label"`
		Icon     string `default:"tsdproxy" validate:"string" yaml:"icon"`
		Category string `validate:"string" yaml:"category"`
		Visible  bool   `default:"true" validate:"boolean" yaml:"visible"`
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
