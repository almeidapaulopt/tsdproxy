// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package tailscale

import (
	"context"
	"fmt"
	"os"
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

// Client struct implements proxyprovider for tailscale
type Client struct {
	log zerolog.Logger

	Hostname     string
	AuthKey      string
	clientID     string
	clientSecret string
	controlURL   string
	datadir      string
	tags         string
}

// stateMeta tracks the configuration used to create the current tsnet state,
// so incompatible config changes can be detected and stale state cleaned up.
type stateMeta struct {
	Ephemeral bool `yaml:"ephemeral"`
}

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

	c.cleanStaleState(config, datadir)
	c.saveStateMeta(config, datadir)

	authKey, err := c.getAuthkey(config)
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

// cleanStaleState removes tsnet state files when configuration has changed
// in ways that make existing state incompatible (e.g. ephemeral flag change).
// Without this cleanup, tsnet reuses stale state that conflicts with the new
// configuration, leaving the node permanently stuck in NeedsLogin.
func (c *Client) cleanStaleState(cfg *model.Config, datadir string) {
	stateFile := filepath.Join(datadir, "tailscaled.state")
	info, err := os.Stat(stateFile)
	if err != nil || info.IsDir() {
		return
	}

	cached := new(stateMeta)
	file := config.NewConfigFile(c.log, path.Join(datadir, "tsdproxy.yaml"), cached)
	if err := file.Load(); err != nil {
		return
	}

	if cached.Ephemeral != cfg.Tailscale.Ephemeral {
		c.log.Info().
			Bool("previous_ephemeral", cached.Ephemeral).
			Bool("current_ephemeral", cfg.Tailscale.Ephemeral).
			Msg("ephemeral setting changed, clearing stale tsnet state")

		if err := os.RemoveAll(datadir); err != nil {
			c.log.Error().Err(err).Msg("failed to clear stale tsnet state")
		}
	}
}

func (c *Client) saveStateMeta(cfg *model.Config, datadir string) {
	meta := &stateMeta{Ephemeral: cfg.Tailscale.Ephemeral}
	file := config.NewConfigFile(c.log, path.Join(datadir, "tsdproxy.yaml"), meta)
	if err := file.Save(); err != nil {
		c.log.Error().Err(err).Msg("failed to save state metadata")
	}
}

// getControlURL method returns the control URL
func (c *Client) getControlURL() string {
	if c.controlURL == "" {
		return model.DefaultTailscaleControlURL
	}
	return c.controlURL
}

func (c *Client) getAuthkey(config *model.Config) (string, error) {
	authKey := config.Tailscale.AuthKey

	if c.clientID != "" && c.clientSecret != "" {
		oauthKey, err := c.getOAuth(config)
		if err != nil {
			return "", fmt.Errorf("getAuthkey: %w", err)
		}
		authKey = oauthKey
	}

	if authKey == "" {
		authKey = c.AuthKey
	}

	if authKey == "" {
		c.log.Info().
			Str("hostname", config.Hostname).
			Msg("No auth key configured, interactive login will be required")
	}

	return authKey, nil
}

func (c *Client) getOAuth(cfg *model.Config) (string, error) {
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

	temptags := c.resolveTags(cfg)

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

	return authkey.Key, nil
}

// resolveTags returns the tags from the proxy config, falling back to the provider config.
func (c *Client) resolveTags(cfg *model.Config) string {
	temptags := strings.Trim(strings.TrimSpace(cfg.Tailscale.Tags), "\"")
	if temptags == "" {
		temptags = strings.Trim(strings.TrimSpace(c.tags), "\"")
	}
	return temptags
}
