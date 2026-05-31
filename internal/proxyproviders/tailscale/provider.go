// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package tailscale

import (
	"context"
	"errors"
	"fmt"
	"path"
	"path/filepath"
	"strings"
	"sync"

	"github.com/almeidapaulopt/tsdproxy/internal/config"
	"github.com/almeidapaulopt/tsdproxy/internal/core/secretstring"
	"github.com/almeidapaulopt/tsdproxy/internal/model"
	"github.com/almeidapaulopt/tsdproxy/internal/proxyproviders"

	"github.com/rs/zerolog"
	"golang.org/x/sync/semaphore"
)

// Client struct implements proxyprovider for tailscale
type Client struct {
	log               zerolog.Logger
	certSem           *semaphore.Weighted
	sharedServer      *SharedServer
	servicesServer    *ServicesServer
	apiFactory        *APIClientFactory
	stateMgr          *StateManager
	deviceReconciler  *DeviceReconciler
	controlURL        string
	AuthKey           secretstring.SecretString
	datadir           string
	tags              string
	Hostname          string
	sharedHostname    string
	sharedMu          sync.Mutex
	preventDuplicates bool
	shared            bool
	services          bool
	autoApprove       bool
}

const userAgent = "tsdproxy"

var (
	_ proxyproviders.Provider               = (*Client)(nil)
	_ proxyproviders.DomainRequiredProvider = (*Client)(nil)
)

func (c *Client) IsDomainRequired() bool {
	return c.shared
}

func New(log zerolog.Logger, name string, provider *config.TailscaleServerConfig) (*Client, error) {
	datadir := filepath.Join(config.Config.Tailscale.DataDir, name)

	concurrency := provider.MaxCertConcurrency
	if concurrency < 1 {
		concurrency = model.DefaultMaxCertConcurrency
	}

	apiFactory := NewAPIClientFactory(
		strings.TrimSpace(provider.ClientID),
		strings.TrimSpace(provider.ClientSecret.Value()),
	)

	clientLog := log.With().Str("tailscale", name).Logger()

	return &Client{
		log:               clientLog,
		Hostname:          name,
		AuthKey:           secretstring.SecretString(strings.TrimSpace(provider.AuthKey.Value())),
		apiFactory:        apiFactory,
		stateMgr:          NewStateManager(clientLog),
		deviceReconciler:  NewDeviceReconciler(clientLog, apiFactory),
		tags:              strings.TrimSpace(provider.Tags),
		datadir:           datadir,
		controlURL:        provider.ControlURL,
		preventDuplicates: provider.PreventDuplicates,
		certSem:           semaphore.NewWeighted(concurrency),
		shared:            provider.Shared,
		services:          provider.Services,
		sharedHostname:    strings.TrimSpace(provider.Hostname),
		autoApprove:       provider.AutoApproveDevices,
	}, nil
}

// ResolveAuthKey resolves the authentication key for the given config.
// Performs OAuth token exchange if configured (per-proxy mode only).
// Shared and services modes return static keys without OAuth.
func (c *Client) ResolveAuthKey(cfg *model.Config) (string, error) {
	// Shared and services modes manage their own auth keys during server startup.
	// Generating a key here wastes one-time OAuth keys and may hit rate limits.
	if c.shared || c.services {
		authKey := cfg.Tailscale.ResolvedAuthKey
		if authKey == "" {
			authKey = cfg.Tailscale.AuthKey.Value()
		}
		if authKey == "" {
			authKey = c.AuthKey.Value()
		}
		return authKey, nil
	}

	authMgr := NewAuthManager(c.log, c.apiFactory, cfg.Tailscale.Ephemeral)
	authKey, err := authMgr.ResolveKey(context.Background(), AuthConfig{
		ResolvedAuthKey: cfg.Tailscale.ResolvedAuthKey,
		ProxyAuthKey:    cfg.Tailscale.AuthKey,
		ProviderAuthKey: c.AuthKey,
	}, c.resolveTags(cfg))
	if err != nil {
		return "", fmt.Errorf("ResolveAuthKey: %w", err)
	}
	return authKey, nil
}

// NewProxy method implements proxyprovider NewProxy method.
func (c *Client) NewProxy(config *model.Config) (proxyproviders.ProxyInterface, error) {
	if c.shared {
		return c.newSharedProxy(config)
	}
	if c.services {
		return c.newServiceProxy(config)
	}

	c.log.Debug().
		Str("hostname", config.Hostname).
		Msg("Setting up tailscale server")

	log := c.log.With().Str("Hostname", config.Hostname).Logger()
	datadir := path.Join(c.datadir, config.Hostname)

	nodeCfg := NodeConfig{
		Hostname:     config.Hostname,
		DataDir:      datadir,
		ControlURL:   c.getControlURL(),
		Tags:         c.resolveTags(config),
		Ephemeral:    config.Tailscale.Ephemeral,
		RunWebClient: config.Tailscale.RunWebClient,
		Verbose:      config.Tailscale.Verbose,
		Mode:         ModePerProxy,
	}

	var deviceReconciler *DeviceReconciler
	if c.preventDuplicates {
		deviceReconciler = c.deviceReconciler
	}

	lifecycle := NewNodeLifecycle(log, NodeLifecycleConfig{
		NodeConfig: nodeCfg,
		AuthConfig: AuthConfig{
			ResolvedAuthKey: config.Tailscale.ResolvedAuthKey,
			ProxyAuthKey:    config.Tailscale.AuthKey,
			ProviderAuthKey: c.AuthKey,
		},
		CertSem:          c.certSem,
		AuthManager:      NewAuthManager(c.log, c.apiFactory, config.Tailscale.Ephemeral),
		StateManager:     c.stateMgr,
		DeviceReconciler: deviceReconciler,
		Retry:            NewRetryPolicy(),
	})

	return &Proxy{
		log:        log,
		config:     config,
		certSem:    c.certSem,
		lifecycle:  lifecycle,
		events:     make(chan model.ProxyEvent, 10), //nolint:mnd
		whoisCache: NewWhoisCache(whoisCacheTTL, whoisCacheMaxEntries),
	}, nil
}

// getControlURL method returns the control URL
func (c *Client) getControlURL() string {
	if c.controlURL == "" {
		return model.DefaultTailscaleControlURL
	}
	return c.controlURL
}

// resolveTags returns the tags from the proxy config, falling back to the provider config.
func (c *Client) resolveTags(cfg *model.Config) string {
	temptags := strings.Trim(strings.TrimSpace(cfg.Tailscale.Tags), "\"")
	if temptags == "" {
		temptags = strings.Trim(strings.TrimSpace(c.tags), "\"")
	}
	return temptags
}

func (c *Client) newSharedProxy(config *model.Config) (proxyproviders.ProxyInterface, error) {
	c.sharedMu.Lock()
	defer c.sharedMu.Unlock()

	if c.sharedServer == nil {
		authMgr := NewAuthManager(c.log, c.apiFactory, config.Tailscale.Ephemeral)

		var deviceReconciler *DeviceReconciler
		if c.preventDuplicates {
			deviceReconciler = c.deviceReconciler
		}

		lifecycleCfg := &NodeLifecycleConfig{
			NodeConfig: NodeConfig{
				Hostname:      c.sharedHostname,
				DataDir:       path.Join(c.datadir, c.sharedHostname),
				ControlURL:    c.getControlURL(),
				Tags:          c.resolveTags(config),
				AdvertiseTags: cleanTags(c.resolveTags(config)),
				Ephemeral:     config.Tailscale.Ephemeral,
				Mode:          ModeShared,
			},
			AuthConfig: AuthConfig{
				ResolvedAuthKey: config.Tailscale.ResolvedAuthKey,
				ProxyAuthKey:    config.Tailscale.AuthKey,
				ProviderAuthKey: c.AuthKey,
			},
			CertSem:          c.certSem,
			AuthManager:      authMgr,
			StateManager:     c.stateMgr,
			DeviceReconciler: deviceReconciler,
			Retry:            NewRetryPolicy(),
		}

		c.sharedServer = NewSharedServer(SharedServerConfig{
			Hostname:        c.sharedHostname,
			DataDir:         path.Join(c.datadir, c.sharedHostname),
			ControlURL:      c.getControlURL(),
			Ephemeral:       config.Tailscale.Ephemeral,
			CertSem:         c.certSem,
			Log:             c.log,
			LifecycleConfig: lifecycleCfg,
		})
	} else if config.Tailscale.Ephemeral != c.sharedServer.ephemeral {
		c.log.Warn().
			Bool("server_ephemeral", c.sharedServer.ephemeral).
			Bool("proxy_ephemeral", config.Tailscale.Ephemeral).
			Str("proxy", config.Hostname).
			Msg("shared server already running with different ephemeral setting; proxy value ignored")
	}

	domain := config.Domain
	if domain == "" {
		return nil, errors.New("shared proxy provider requires a domain to be set on each proxy")
	}

	return &SharedProxy{
		log:    c.log.With().Str("Hostname", config.Hostname).Str("domain", domain).Logger(),
		config: config,
		shared: c.sharedServer,
		domain: domain,
		events: make(chan model.ProxyEvent, 10), //nolint:mnd
	}, nil
}

func (c *Client) newServiceProxy(config *model.Config) (proxyproviders.ProxyInterface, error) {
	c.sharedMu.Lock()
	defer c.sharedMu.Unlock()

	if config.Domain != "" {
		return nil, errors.New("services mode does not support custom domains; VIP Services assign FQDNs automatically")
	}

	if c.servicesServer == nil {
		authMgr := NewAuthManager(c.log, c.apiFactory, config.Tailscale.Ephemeral)

		tags := c.resolveTags(config)

		var deviceReconciler *DeviceReconciler
		if c.preventDuplicates {
			deviceReconciler = c.deviceReconciler
		}

		lifecycleCfg := &NodeLifecycleConfig{
			NodeConfig: NodeConfig{
				Hostname:      c.sharedHostname,
				DataDir:       path.Join(c.datadir, c.sharedHostname),
				ControlURL:    c.getControlURL(),
				Tags:          tags,
				AdvertiseTags: cleanTags(tags),
				Ephemeral:     config.Tailscale.Ephemeral,
				Mode:          ModeServices,
			},
			AuthConfig: AuthConfig{
				ResolvedAuthKey: config.Tailscale.ResolvedAuthKey,
				ProxyAuthKey:    config.Tailscale.AuthKey,
				ProviderAuthKey: c.AuthKey,
			},
			CertSem:          c.certSem,
			AuthManager:      authMgr,
			StateManager:     c.stateMgr,
			DeviceReconciler: deviceReconciler,
			Retry:            NewRetryPolicy(),
		}

		c.servicesServer = NewServicesServer(ServicesServerConfig{
			Hostname:           c.sharedHostname,
			DataDir:            path.Join(c.datadir, c.sharedHostname),
			ControlURL:         c.getControlURL(),
			Ephemeral:          config.Tailscale.Ephemeral,
			APIFactory:         c.apiFactory,
			AuthManager:        authMgr,
			Tags:               tags,
			Log:                c.log,
			DeviceReconciler:   deviceReconciler,
			LifecycleConfig:    lifecycleCfg,
			AutoApproveDevices: c.autoApprove,
		})
	} else if config.Tailscale.Ephemeral != c.servicesServer.ephemeral {
		c.log.Warn().
			Bool("server_ephemeral", c.servicesServer.ephemeral).
			Bool("proxy_ephemeral", config.Tailscale.Ephemeral).
			Str("proxy", config.Hostname).
			Msg("services server already running with different ephemeral setting; proxy value ignored")
	}

	serviceName := "svc:" + config.Hostname

	return &ServiceProxy{
		log:         c.log.With().Str("Hostname", config.Hostname).Str("service", serviceName).Logger(),
		config:      config,
		services:    c.servicesServer,
		serviceName: serviceName,
		events:      make(chan model.ProxyEvent, 10), //nolint:mnd
	}, nil
}
