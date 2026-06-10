// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package tailscale

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/almeidapaulopt/tsdproxy/internal/config"
	"github.com/almeidapaulopt/tsdproxy/internal/core/secretstring"
	"github.com/almeidapaulopt/tsdproxy/internal/model"
	"github.com/almeidapaulopt/tsdproxy/internal/proxyproviders"

	"github.com/rs/zerolog"
	"golang.org/x/sync/semaphore"
)

// Client struct implements proxyprovider for tailscale
type Client struct {
	log                 zerolog.Logger
	providerCtx         context.Context
	certSem             *semaphore.Weighted
	sharedServer        *SharedServer
	servicesServer      *ServicesServer
	apiFactory          *APIClientFactory
	stateMgr            *StateManager
	deviceReconciler    *DeviceReconciler
	providerCancel      context.CancelFunc
	tags                string
	Hostname            string
	sharedHostname      string
	datadir             string
	AuthKey             secretstring.SecretString
	controlURL          string
	authRetry           config.AuthRetryConfig
	authRetryInit       time.Duration
	authRetryMax        time.Duration
	reconcileInterval   time.Duration
	sharedMu            sync.Mutex
	preventDuplicates   bool
	shared              bool
	services            bool
	autoApprove         bool
	autoRemoveConflicts bool
}

const (
	userAgent = "tsdproxy"

	defaultAuthRetryInitDelay = 2 * time.Second
	defaultAuthRetryMaxDelay  = 30 * time.Second
	proxyEventBufferSize      = 10
)

// validateDatadir joins baseDir and hostname into a path, then verifies the result
// does not escape baseDir via path traversal (e.g. "../../etc").
func validateDatadir(baseDir, hostname string) (string, error) {
	if strings.ContainsRune(hostname, 0) {
		return "", fmt.Errorf("hostname %q contains null byte", hostname)
	}
	if filepath.IsAbs(hostname) {
		return "", fmt.Errorf("hostname %q is an absolute path", hostname)
	}

	datadir := filepath.Join(baseDir, hostname)
	cleanDir := filepath.Clean(datadir)
	rel, err := filepath.Rel(filepath.Clean(baseDir), cleanDir)
	if err != nil {
		return "", fmt.Errorf("hostname %q results in invalid path: %w", hostname, err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("hostname %q results in path escaping data directory", hostname)
	}
	return datadir, nil
}

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
		provider.ClientSecret,
	)

	clientLog := log.With().Str("tailscale", name).Logger()

	if apiFactory.IsAvailable() {
		scopes := ScopesPerProxy()
		if provider.Services {
			scopes = ScopesServices()
		}
		validateCtx, validateCancel := context.WithTimeout(context.Background(), apiTimeout)
		if err := apiFactory.ValidateAccess(validateCtx, scopes); err != nil {
			validateCancel()
			return nil, fmt.Errorf("new tailscale provider %q: %w", name, err)
		}
		validateCancel()
		clientLog.Info().Strs("scopes", scopes).Msg("OAuth credentials validated")
	}

	reconcileInterval, err := time.ParseDuration(provider.ReconcileInterval)
	if err != nil {
		clientLog.Warn().Err(err).Str("value", provider.ReconcileInterval).
			Msg("invalid reconcileInterval, disabling periodic reconciliation")
		reconcileInterval = 0
	}

	authRetryInit, err := time.ParseDuration(provider.AuthRetry.InitialBackoff)
	if err != nil {
		clientLog.Warn().Err(err).Str("value", provider.AuthRetry.InitialBackoff).
			Msg("invalid authRetry.initialBackoff, using default 2s")
		authRetryInit = defaultAuthRetryInitDelay
	}
	authRetryMax, err := time.ParseDuration(provider.AuthRetry.MaxBackoff)
	if err != nil {
		clientLog.Warn().Err(err).Str("value", provider.AuthRetry.MaxBackoff).
			Msg("invalid authRetry.maxBackoff, using default 30s")
		authRetryMax = defaultAuthRetryMaxDelay
	}

	preventDuplicates := provider.PreventDuplicates
	if preventDuplicates && (provider.ClientID == "" || provider.ClientSecret.Value() == "") {
		clientLog.Warn().
			Msg("preventDuplicates is enabled but OAuth credentials (clientId/clientSecret) are not configured. " +
				"Duplicate prevention requires OAuth. Disabling preventDuplicates.")
		preventDuplicates = false
	}

	providerCtx, providerCancel := context.WithCancel(context.Background()) //nolint:gosec // cancel stored in Client struct, called on shutdown

	c := &Client{
		log:                 clientLog,
		Hostname:            name,
		AuthKey:             secretstring.SecretString(strings.TrimSpace(provider.AuthKey.Value())),
		apiFactory:          apiFactory,
		stateMgr:            NewStateManager(clientLog),
		deviceReconciler:    NewDeviceReconciler(clientLog, apiFactory),
		tags:                strings.TrimSpace(provider.Tags),
		datadir:             datadir,
		controlURL:          provider.ControlURL,
		preventDuplicates:   preventDuplicates,
		authRetry:           provider.AuthRetry,
		authRetryInit:       authRetryInit,
		authRetryMax:        authRetryMax,
		certSem:             semaphore.NewWeighted(concurrency),
		shared:              provider.Shared,
		services:            provider.Services,
		sharedHostname:      strings.TrimSpace(provider.Hostname),
		autoApprove:         provider.AutoApproveDevices,
		autoRemoveConflicts: provider.AutoRemoveConflicts,
		reconcileInterval:   reconcileInterval,
		providerCtx:         providerCtx,
		providerCancel:      providerCancel,
	}

	// Start periodic device reconciliation goroutine if interval is configured
	// and the provider is in shared or services mode (per-proxy mode handles
	// reconciliation at each proxy's startup via NodeLifecycle.Start).
	if reconcileInterval > 0 && (c.shared || c.services) {
		go c.runPeriodicReconcile()
	}

	return c, nil
}

// runPeriodicReconcile periodically reconciles stale devices at the configured interval.
func (c *Client) runPeriodicReconcile() {
	c.log.Debug().
		Dur("interval", c.reconcileInterval).
		Msg("starting periodic device reconciliation")

	ticker := time.NewTicker(c.reconcileInterval)
	defer ticker.Stop()

	for {
		select {
		case <-c.providerCtx.Done():
			c.log.Debug().Msg("stopping periodic device reconciliation")
			return
		case <-ticker.C:
			c.log.Debug().Msg("running periodic device reconciliation")
			reconcileCtx, cancel := context.WithTimeout(c.providerCtx, apiTimeout)
			c.deviceReconciler.Reconcile(reconcileCtx, c.sharedHostname, c.tags, nil)
			cancel()
		}
	}
}

func (c *Client) Close() {
	c.providerCancel()
	if c.sharedServer != nil {
		c.sharedServer.Close()
	}
	if c.servicesServer != nil {
		c.servicesServer.Close()
	}
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
	resolveCtx, resolveCancel := context.WithTimeout(context.Background(), apiTimeout)
	defer resolveCancel()
	authKey, err := authMgr.ResolveKey(resolveCtx, AuthConfig{
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
	datadir, err := validateDatadir(c.datadir, config.Hostname)
	if err != nil {
		return nil, fmt.Errorf("new proxy: %w", err)
	}

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

	lifecycle := NewNodeLifecycle(log, c.buildLifecycleConfig(config, nodeCfg))

	return &Proxy{
		log:        log,
		config:     config,
		certSem:    c.certSem,
		lifecycle:  lifecycle,
		exposure:   NewPerProxyExposure(log),
		events:     make(chan model.ProxyEvent, proxyEventBufferSize),
		whoisCache: NewWhoisCache(whoisCacheTTL, whoisCacheMaxEntries),
	}, nil
}

// buildLifecycleConfig creates a NodeLifecycleConfig with common fields
// derived from the proxy config and provider settings.
func (c *Client) buildLifecycleConfig(config *model.Config, nodeCfg NodeConfig) NodeLifecycleConfig {
	var deviceReconciler *DeviceReconciler
	if c.preventDuplicates {
		deviceReconciler = c.deviceReconciler
	}

	// Resolve auth retry policy from provider config.
	maxAttempts := c.authRetry.MaxAttempts
	if !c.authRetry.Enabled {
		maxAttempts = 0
	}

	return NodeLifecycleConfig{
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
		Retry:            NewRetryPolicyFromConfig(maxAttempts, c.authRetryInit, c.authRetryMax),
	}
}

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
		sharedDatadir, err := validateDatadir(c.datadir, c.sharedHostname)
		if err != nil {
			return nil, fmt.Errorf("new shared proxy: %w", err)
		}

		tags := c.resolveTags(config)

		lifecycleCfg := c.buildLifecycleConfig(config, NodeConfig{
			Hostname:      c.sharedHostname,
			DataDir:       sharedDatadir,
			ControlURL:    c.getControlURL(),
			Tags:          tags,
			AdvertiseTags: cleanTags(tags),
			Ephemeral:     config.Tailscale.Ephemeral,
			Mode:          ModeShared,
		})

		c.sharedServer = NewSharedServer(SharedServerConfig{
			Hostname:        c.sharedHostname,
			DataDir:         sharedDatadir,
			ControlURL:      c.getControlURL(),
			Ephemeral:       config.Tailscale.Ephemeral,
			CertSem:         c.certSem,
			Log:             c.log,
			LifecycleConfig: &lifecycleCfg,
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
		log:      c.log.With().Str("Hostname", config.Hostname).Str("domain", domain).Logger(),
		config:   config,
		shared:   c.sharedServer,
		exposure: NewSharedSNIExposure(c.sharedServer, domain),
		domain:   domain,
		events:   make(chan model.ProxyEvent, proxyEventBufferSize),
	}, nil
}

func (c *Client) newServiceProxy(config *model.Config) (proxyproviders.ProxyInterface, error) {
	c.sharedMu.Lock()
	defer c.sharedMu.Unlock()

	if config.Domain != "" {
		return nil, errors.New("services mode does not support custom domains; VIP Services assign FQDNs automatically")
	}

	if c.servicesServer == nil {
		sharedDatadir, err := validateDatadir(c.datadir, c.sharedHostname)
		if err != nil {
			return nil, fmt.Errorf("new service proxy: %w", err)
		}

		tags := c.resolveTags(config)

		lifecycleCfg := c.buildLifecycleConfig(config, NodeConfig{
			Hostname:      c.sharedHostname,
			DataDir:       sharedDatadir,
			ControlURL:    c.getControlURL(),
			Tags:          tags,
			AdvertiseTags: cleanTags(tags),
			Ephemeral:     config.Tailscale.Ephemeral,
			Mode:          ModeServices,
		})

		c.servicesServer = NewServicesServer(ServicesServerConfig{
			Hostname:            c.sharedHostname,
			DataDir:             sharedDatadir,
			ControlURL:          c.getControlURL(),
			Ephemeral:           config.Tailscale.Ephemeral,
			APIFactory:          c.apiFactory,
			AuthManager:         lifecycleCfg.AuthManager,
			Tags:                tags,
			Log:                 c.log,
			CertSem:             c.certSem,
			DeviceReconciler:    lifecycleCfg.DeviceReconciler,
			LifecycleConfig:     &lifecycleCfg,
			AutoApproveDevices:  c.autoApprove,
			AutoRemoveConflicts: c.autoRemoveConflicts,
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
		exposure:    NewServicesVIPExposure(c.servicesServer, serviceName),
		serviceName: serviceName,
		events:      make(chan model.ProxyEvent, proxyEventBufferSize),
	}, nil
}
