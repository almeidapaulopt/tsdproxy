// SPDX-FileCopyrightText: 2024 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT
package tailscale

import (
	"path"
	"path/filepath"
	"strings"

	"github.com/rs/zerolog"
	"tailscale.com/tsnet"

	"github.com/almeidapaulopt/tsdproxy/internal/config"
	"github.com/almeidapaulopt/tsdproxy/internal/proxyconfig"
	"github.com/almeidapaulopt/tsdproxy/internal/proxyproviders"
)

// Client struct implements proxyprovider for tailscale
type Client struct {
	log zerolog.Logger

	Hostname   string
	AuthKey    string
	controlURL string
	datadir    string
}

func New(log zerolog.Logger, name string, provider *config.TailscaleServerConfig) (*Client, error) {
	datadir := filepath.Join(config.Config.Tailscale.DataDir, name)

	return &Client{
		log:      log.With().Str("tailscale", name).Logger(),
		Hostname: name,
		// make sure the auth key is trimmed
		AuthKey:    strings.TrimSpace(provider.AuthKey),
		datadir:    datadir,
		controlURL: provider.ControlURL,
	}, nil
}

// NewProxy method implements proxyprovider NewProxy method
func (c *Client) NewProxy(config *proxyconfig.Config) (proxyproviders.Proxy, error) {
	c.log.Debug().
		Str("hostname", config.Hostname).
		Bool("ephemeral", config.Tailscale.Ephemeral).
		Bool("runWebClient", config.Tailscale.RunWebClient).
		Msg("Setting up tailscale server")

	// If the auth key is not set, use the provider auth key
	authKey := config.Tailscale.AuthKey
	if authKey == "" {
		authKey = c.AuthKey
	}

	datadir := path.Join(c.datadir, config.Hostname)

	tserver := &tsnet.Server{
		Hostname:     config.Hostname,
		AuthKey:      authKey,
		Dir:          datadir,
		Ephemeral:    config.Tailscale.Ephemeral,
		RunWebClient: config.Tailscale.RunWebClient,
		UserLogf: func(format string, args ...any) {
			c.log.Info().Msgf(format, args...)
		},
		Logf: func(format string, args ...any) {
			c.log.Trace().Msgf(format, args...)
		},

		ControlURL: c.getControlURL(config),
	}

	// if verbose is set, use the info log level
	if config.Tailscale.Verbose {
		tserver.Logf = func(format string, args ...any) {
			c.log.Info().Msgf(format, args...)
		}
	}

	return &Proxy{
		log:      c.log,
		config:   config,
		tsServer: tserver,
	}, nil
}

// getControlURL method returns the control URL
func (c *Client) getControlURL(cfg *proxyconfig.Config) string {
	if cfg.Tailscale.ControlURL == "" {
		return proxyconfig.TailscaleControlURL
	}
	return cfg.Tailscale.ControlURL
}
