// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package tailscale

import (
	"context"
	"fmt"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/almeidapaulopt/tsdproxy/internal/config"
	"github.com/almeidapaulopt/tsdproxy/internal/model"
	"github.com/almeidapaulopt/tsdproxy/internal/proxyproviders"

	"github.com/rs/zerolog"
	"tailscale.com/client/tailscale/v2"
	"tailscale.com/tsnet"
)

type (
	// Client struct implements proxyprovider for tailscale
	Client struct {
		log zerolog.Logger

		Hostname     string
		AuthKey      string
		clientID     string
		clientSecret string
		controlURL   string
		datadir      string
		tags         string
	}

	oauth struct {
		Authkey string `yaml:"authkey"`
	}
)

var _ proxyproviders.Provider = (*Client)(nil)

func New(log zerolog.Logger, name string, provider *config.TailscaleServerConfig) (*Client, error) {
	datadir := filepath.Join(config.Config.Tailscale.DataDir, name)

	return &Client{
		log:          log.With().Str("tailscale", name).Logger(),
		Hostname:     name,
		AuthKey:      strings.TrimSpace(provider.AuthKey),
		clientID:     strings.TrimSpace(provider.ClientID),
		clientSecret: strings.TrimSpace(provider.ClientSecret),
		tags:         strings.TrimSpace(provider.Tags),
		datadir:      datadir,
		controlURL:   provider.ControlURL,
	}, nil
}

// NewProxy method implements proxyprovider NewProxy method
func (c *Client) NewProxy(config *model.Config) (proxyproviders.ProxyInterface, error) {
	c.log.Debug().
		Str("hostname", config.Hostname).
		Msg("Setting up tailscale server")

	log := c.log.With().Str("Hostname", config.Hostname).Logger()

	datadir := path.Join(c.datadir, config.Hostname)
	authKey, err := c.getAuthkey(config, datadir)
	if err != nil {
		return nil, fmt.Errorf("tailscale NewProxy: %w", err)
	}

	tserver := &tsnet.Server{
		Hostname:     config.Hostname,
		AuthKey:      authKey,
		Dir:          datadir,
		Ephemeral:    config.Tailscale.Ephemeral,
		RunWebClient: config.Tailscale.RunWebClient,
		UserLogf: func(format string, args ...any) {
			log.Info().Msgf(format, args...)
		},
		Logf: func(format string, args ...any) {
			log.Trace().Msgf(format, args...)
		},

		ControlURL: c.getControlURL(),
	}

	// if verbose is set, use the info log level
	if config.Tailscale.Verbose {
		tserver.Logf = func(format string, args ...any) {
			log.Info().Msgf(format, args...)
		}
	}

	return &Proxy{
		log:      log,
		config:   config,
		tsServer: tserver,
		events:   make(chan model.ProxyEvent, 10), //nolint:mnd
	}, nil
}

// getControlURL method returns the control URL
func (c *Client) getControlURL() string {
	if c.controlURL == "" {
		return model.DefaultTailscaleControlURL
	}
	return c.controlURL
}

func (c *Client) getAuthkey(config *model.Config, path string) (string, error) {
	authKey := config.Tailscale.AuthKey

	if c.clientID != "" && c.clientSecret != "" {
		oauthKey, err := c.getOAuth(config, path)
		if err != nil {
			return "", fmt.Errorf("getAuthkey: %w", err)
		}
		authKey = oauthKey
	}

	if authKey == "" {
		authKey = c.AuthKey
	}

	if authKey == "" {
		return "", fmt.Errorf("no auth key configured for %s", config.Hostname)
	}

	return authKey, nil
}

func (c *Client) getOAuth(cfg *model.Config, dir string) (string, error) {
	data := new(oauth)

	file := config.NewConfigFile(c.log, path.Join(dir, "tsdproxy.yaml"), data)
	if err := file.Load(); err == nil {
		if data.Authkey != "" {
			return data.Authkey, nil
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second) //nolint:mnd
	defer cancel()

	tsclient := &tailscale.Client{
		Tailnet:   "-",
		UserAgent: "tsdproxy",
		HTTP: tailscale.OAuthConfig{
			ClientID:     c.clientID,
			ClientSecret: c.clientSecret,
			Scopes:       []string{"all:write"},
		}.HTTPClient(),
	}

	temptags := strings.Trim(strings.TrimSpace(cfg.Tailscale.Tags), "\"")
	if temptags == "" {
		temptags = strings.Trim(strings.TrimSpace(c.tags), "\"")
	}

	if temptags == "" {
		return "", fmt.Errorf("must define tags to use OAuth")
	}

	capabilities := tailscale.KeyCapabilities{}
	capabilities.Devices.Create.Ephemeral = cfg.Tailscale.Ephemeral
	capabilities.Devices.Create.Reusable = false
	capabilities.Devices.Create.Preauthorized = true
	capabilities.Devices.Create.Tags = strings.Split(temptags, ",")

	ckr := tailscale.CreateKeyRequest{
		Capabilities: capabilities,
		Description:  "tsdproxy",
	}

	authkey, err := tsclient.Keys().Create(ctx, ckr)
	if err != nil {
		return "", fmt.Errorf("unable to get OAuth token: %w", err)
	}

	data.Authkey = authkey.Key
	if err := file.Save(); err != nil {
		c.log.Error().Err(err).Msg("unable to save oauth file")
	}

	return authkey.Key, nil
}
