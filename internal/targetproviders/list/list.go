// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package list

import (
	"context"
	"fmt"
	"maps"
	"net/url"
	"reflect"
	"sync"

	"github.com/almeidapaulopt/tsdproxy/internal/config"
	"github.com/almeidapaulopt/tsdproxy/internal/model"
	"github.com/almeidapaulopt/tsdproxy/internal/targetproviders"

	"github.com/creasty/defaults"
	"github.com/fsnotify/fsnotify"
	"github.com/rs/zerolog"
)

type (
	// Client struct implements TargetProvider
	Client struct {
		log           zerolog.Logger
		file          *config.File
		configProxies configProxyList
		proxies       configProxyList
		eventsChan    chan targetproviders.TargetEvent
		errChan       chan error
		name          string
		config        config.ListTargetProviderConfig
		mtx           sync.RWMutex
	}

	configProxyList map[string]proxyConfig

	proxyConfig struct {
		Ports           map[string]port `yaml:"ports"`
		ProxyProvider   string          `yaml:"proxyProvider"`
		Domain          string          `yaml:"domain"`
		DNSProvider     string          `yaml:"dnsProvider"`
		TLSProvider     string          `yaml:"tlsProvider"`
		Dashboard       model.Dashboard `validate:"dive" yaml:"dashboard"`
		Tailscale       model.Tailscale `yaml:"tailscale"`
		IdentityHeaders bool            `default:"true" validate:"boolean" yaml:"identityHeaders"`
	}

	port struct {
		Targets     []string            `yaml:"targets,omitempty"`
		Tailscale   model.TailscalePort `validate:"dive" yaml:"tailscale"`
		IsRedirect  bool                `default:"false" validate:"boolean" yaml:"isRedirect,omitempty"`
		TLSValidate bool                `validate:"boolean" default:"true" yaml:"tlsValidate"`
	}

	ProxyConfigAPI struct {
		Dashboard     DashboardAPI       `yaml:"dashboard"`
		Ports         map[string]PortAPI `yaml:"ports"`
		ProxyProvider string             `yaml:"proxyProvider,omitempty"`
		Tailscale     TailscaleAPI       `yaml:"tailscale"`
	}

	DashboardAPI struct {
		Label    string `yaml:"label,omitempty"`
		Icon     string `yaml:"icon,omitempty"`
		Category string `yaml:"category,omitempty"`
		Visible  bool   `yaml:"visible,omitempty"`
	}

	TailscaleAPI struct {
		Tags         string `yaml:"tags,omitempty"`
		Ephemeral    bool   `yaml:"ephemeral,omitempty"`
		RunWebClient bool   `yaml:"runWebClient,omitempty"`
	}

	PortAPI struct {
		Targets     []string         `yaml:"targets"`
		TLSValidate bool             `yaml:"tlsValidate"`
		IsRedirect  bool             `yaml:"isRedirect,omitempty"`
		Tailscale   TailscalePortAPI `yaml:"tailscale,omitempty"`
	}

	TailscalePortAPI struct {
		Funnel bool `yaml:"funnel,omitempty"`
	}
)

var _ targetproviders.TargetProvider = (*Client)(nil)

func (s *proxyConfig) UnmarshalYAML(unmarshal func(any) error) error {
	_ = defaults.Set(s)

	type plain proxyConfig
	if err := unmarshal((*plain)(s)); err != nil {
		return err
	}

	return nil
}

// New function returns a new Files TargetProvider
func New(log zerolog.Logger, name string, provider *config.ListTargetProviderConfig) (*Client, error) {
	newlog := log.With().Str("file", name).Logger()

	proxiesList := configProxyList{}

	file := config.NewConfigFile(newlog, provider.Filename, proxiesList)
	err := file.Load()
	if err != nil {
		return nil, fmt.Errorf("error reading config: %w", err)
	}

	c := &Client{
		file:          file,
		log:           newlog,
		name:          name,
		configProxies: proxiesList,
		proxies:       make(map[string]proxyConfig),
		eventsChan:    make(chan targetproviders.TargetEvent),
		errChan:       make(chan error),
		config:        *provider,
	}

	// load default values
	err = defaults.Set(c)
	if err != nil {
		return nil, fmt.Errorf("error loading defaults: %w", err)
	}

	return c, nil
}

func (c *Client) WatchEvents(ctx context.Context, eventsChan chan targetproviders.TargetEvent, errChan chan error) {
	c.log.Debug().Msg("Start WatchEvents")

	c.eventsChan = eventsChan
	c.errChan = errChan

	c.file.OnChange(c.onFileChange)

	if err := c.file.Watch(); err != nil {
		select {
		case <-ctx.Done():
		case errChan <- err:
		}
		return
	}

	go func() {
		for k := range c.configProxies {
			select {
			case <-ctx.Done():
				return
			case eventsChan <- targetproviders.TargetEvent{
				ID:             k,
				TargetProvider: c,
				Action:         targetproviders.ActionStartProxy,
			}:
			}
		}
	}()
}

func (c *Client) GetDefaultProxyProviderName() string {
	return c.config.DefaultProxyProvider
}

func (c *Client) trySendEvent(id string, action targetproviders.ActionType) bool {
	select {
	case c.eventsChan <- targetproviders.TargetEvent{
		ID:             id,
		TargetProvider: c,
		Action:         action,
	}:
		return true
	default:
		c.log.Warn().Str("name", id).Int("action", int(action)).Msg("dropped event: channel full")
		return false
	}
}

func (c *Client) Close() {
	c.mtx.RLock()
	names := make([]string, 0, len(c.proxies))
	for name := range c.proxies {
		names = append(names, name)
	}
	c.mtx.RUnlock()

	for _, name := range names {
		c.trySendEvent(name, targetproviders.ActionStopProxy)
	}
}

func (c *Client) AddTarget(id string) (*model.Config, error) {
	c.mtx.RLock()
	proxy, ok := c.configProxies[id]
	c.mtx.RUnlock()

	if !ok {
		return nil, fmt.Errorf("target %s not found", id)
	}

	pcfg, err := c.newProxyConfig(id, proxy)
	if err != nil {
		return nil, err
	}

	return pcfg, nil
}

func (c *Client) DeleteProxy(id string) error {
	c.mtx.Lock()
	defer c.mtx.Unlock()

	if _, ok := c.proxies[id]; !ok {
		return fmt.Errorf("target %s not found", id)
	}

	delete(c.proxies, id)

	return nil
}

// ReResolve re-reads the proxy config for the given target ID.
// Returns the same result as AddTarget — used by health-triggered re-resolution.
func (c *Client) ReResolve(id string) (*model.Config, error) {
	c.log.Trace().Msgf("ReResolve %s", id)
	defer c.log.Trace().Msgf("End ReResolve %s", id)

	c.mtx.RLock()
	proxy, ok := c.configProxies[id]
	c.mtx.RUnlock()

	if !ok {
		return nil, fmt.Errorf("target %s not found", id)
	}

	return c.buildConfig(id, proxy)
}

func (c *Client) buildConfig(id string, p proxyConfig) (*model.Config, error) {
	proxyProvider := c.config.DefaultProxyProvider
	if p.ProxyProvider != "" {
		proxyProvider = p.ProxyProvider
	}

	pcfg, err := model.NewConfig()
	if err != nil {
		return nil, err
	}

	pcfg.TargetID = id
	pcfg.Hostname = id
	pcfg.TargetProvider = c.name
	pcfg.Tailscale = p.Tailscale
	pcfg.ProxyProvider = proxyProvider
	pcfg.ProxyAccessLog = c.config.DefaultProxyAccessLog
	pcfg.IdentityHeaders = model.DefaultIdentityHeaders
	pcfg.AutoRestart = c.config.AutoRestart
	pcfg.HealthCheckEnabled = c.config.HealthCheckEnabled
	pcfg.HealthCheckInterval = c.config.HealthCheckInterval
	pcfg.HealthCheckFailures = c.config.HealthCheckFailures
	pcfg.HealthCheckCooldown = c.config.HealthCheckCooldown
	pcfg.Ports = c.getPorts(p.Ports)
	pcfg.Dashboard = p.Dashboard
	pcfg.Domain = p.Domain
	pcfg.DNSProvider = p.DNSProvider
	pcfg.TLSProvider = p.TLSProvider

	return pcfg, nil
}

// newProxyConfig method returns a new proxyconfig.Config
func (c *Client) newProxyConfig(name string, p proxyConfig) (*model.Config, error) {
	pcfg, err := c.buildConfig(name, p)
	if err != nil {
		return nil, err
	}

	c.addTarget(p, name)

	return pcfg, nil
}

func (c *Client) onFileChange(_ fsnotify.Event) {
	c.log.Info().Msg("config changed, reloading")

	var stops, starts, restarts []string

	c.mtx.Lock()
	oldConfigProxies := maps.Clone(c.configProxies)

	for k := range c.configProxies {
		delete(c.configProxies, k)
	}
	if err := c.file.Load(); err != nil {
		c.log.Error().Err(err).Msg("error loading config")
		for k := range oldConfigProxies {
			c.configProxies[k] = oldConfigProxies[k]
		}
		c.mtx.Unlock()
		return
	}

	for name := range oldConfigProxies {
		if _, ok := c.configProxies[name]; !ok {
			stops = append(stops, name)
		}
	}

	for name := range c.configProxies {
		if _, ok := oldConfigProxies[name]; !ok {
			starts = append(starts, name)
			continue
		}
		if !reflect.DeepEqual(c.configProxies[name], oldConfigProxies[name]) {
			restarts = append(restarts, name)
		}
	}
	c.mtx.Unlock()

	for _, name := range stops {
		c.trySendEvent(name, targetproviders.ActionStopProxy)
	}

	for _, name := range starts {
		c.trySendEvent(name, targetproviders.ActionStartProxy)
	}

	for _, name := range restarts {
		c.trySendEvent(name, targetproviders.ActionRestartProxy)
	}
}

// addTarget method add a target the proxies map
func (c *Client) addTarget(cfg proxyConfig, name string) {
	c.mtx.Lock()
	defer c.mtx.Unlock()

	c.proxies[name] = cfg
}

// getPorts returns a map of PortConfig from the config
func (c *Client) getPorts(l map[string]port) model.PortConfigList {
	ports := make(model.PortConfigList)
	for k, v := range l {
		if model.IsPortRangeShortLabel(k) {
			c.processPortRange(ports, k, v)
			continue
		}

		c.processSinglePort(ports, k, v)
	}
	return ports
}

func (c *Client) processPortRange(ports model.PortConfigList, k string, v port) {
	expanded, err := model.ExpandPortRangeShortLabel(k)
	if err != nil {
		c.log.Error().Err(err).Str("port", k).Msg("error expanding port range")
		return
	}

	for rangeKey, portCfg := range expanded {
		cfg := portCfg
		cfg.IsRedirect = v.IsRedirect

		if !c.parseAndAddTargets(&cfg, v.Targets, k, "no targets found for range port") {
			continue
		}

		cfg.TLSValidate = v.TLSValidate
		cfg.Tailscale = v.Tailscale

		expandedKey := k + "." + rangeKey
		ports[expandedKey] = cfg
	}
}

func (c *Client) processSinglePort(ports model.PortConfigList, k string, v port) {
	port, err := model.NewPortShortLabel(k)
	if err != nil {
		c.log.Error().Err(err).Str("port", k).Msg("error creating port config")
	}

	port.IsRedirect = v.IsRedirect

	if !c.parseAndAddTargets(&port, v.Targets, k, "no targets found for port") {
		return
	}

	port.TLSValidate = v.TLSValidate
	port.Tailscale = v.Tailscale

	ports[k] = port
}

func (c *Client) parseAndAddTargets(cfg *model.PortConfig, targets []string, portKey string, noTargetsMsg string) bool {
	for _, target := range targets {
		targetURL, err := url.Parse(target)
		if err != nil || targetURL.Scheme == "" || targetURL.Host == "" {
			c.log.Error().Err(err).Str("port", portKey).Str("targetUrl", target).Msg("Invalid target URL")
			continue
		}

		cfg.AddTarget(targetURL)
	}

	if len(cfg.GetTargets()) == 0 {
		c.log.Error().Str("port", portKey).Msg(noTargetsMsg)
		return false
	}

	return true
}
