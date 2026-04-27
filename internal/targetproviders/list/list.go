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
		file          *config.ConfigFile
		configProxies configProxyList
		proxies       configProxyList
		eventsChan    chan targetproviders.TargetEvent
		errChan       chan error
		name          string
		config        config.ListTargetProviderConfig
		mtx           sync.Mutex
	}

	configProxyList map[string]proxyConfig

	proxyConfig struct {
		Dashboard     model.Dashboard `validate:"dive" yaml:"dashboard"`
		Ports         map[string]port `yaml:"ports"`
		ProxyProvider string          `yaml:"proxyProvider"`
		Tailscale     model.Tailscale `yaml:"tailscale"`
	}

	port struct {
		Targets     []string            `yaml:"targets,omitempty"`
		Tailscale   model.TailscalePort `validate:"dive" yaml:"tailscale"`
		IsRedirect  bool                `default:"false" validate:"boolean" yaml:"isRedirect,omitempty"`
		TLSValidate bool                `validate:"boolean" default:"true" yaml:"tlsValidate"`
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
	}

	// load default values
	err = defaults.Set(c)
	if err != nil {
		return nil, fmt.Errorf("error loading defaults: %w", err)
	}

	return c, nil
}

func (c *Client) WatchEvents(_ context.Context, eventsChan chan targetproviders.TargetEvent, errChan chan error) {
	c.log.Debug().Msg("Start WatchEvents")

	c.eventsChan = eventsChan
	c.errChan = errChan

	c.file.Watch()
	c.file.OnChange(c.onFileChange)

	// start initial proxies
	go func() {
		for k := range c.configProxies {
			eventsChan <- targetproviders.TargetEvent{
				ID:             k,
				TargetProvider: c,
				Action:         targetproviders.ActionStartProxy,
			}
		}
	}()
}

func (c *Client) GetDefaultProxyProviderName() string {
	return c.config.DefaultProxyProvider
}

func (c *Client) Close() {
	for name := range c.proxies {
		c.eventsChan <- targetproviders.TargetEvent{
			ID:             name,
			TargetProvider: c,
			Action:         targetproviders.ActionStopProxy,
		}
	}
}

func (c *Client) AddTarget(id string) (*model.Config, error) {
	proxy, ok := c.configProxies[id]
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

// newProxyConfig method returns a new proxyconfig.Config
func (c *Client) newProxyConfig(name string, p proxyConfig) (*model.Config, error) {
	proxyProvider := c.config.DefaultProxyProvider
	if p.ProxyProvider != "" {
		proxyProvider = p.ProxyProvider
	}

	proxyAccessLog := model.DefaultProxyAccessLog

	pcfg, err := model.NewConfig()
	if err != nil {
		return nil, err
	}

	pcfg.TargetID = name
	pcfg.Hostname = name
	pcfg.TargetProvider = c.name
	pcfg.Tailscale = p.Tailscale
	pcfg.ProxyProvider = proxyProvider
	pcfg.ProxyAccessLog = proxyAccessLog
	pcfg.Ports = c.getPorts(p.Ports)
	pcfg.Dashboard = p.Dashboard

	c.addTarget(p, name)

	return pcfg, nil
}

func (c *Client) onFileChange(e fsnotify.Event) {
	if !e.Op.Has(fsnotify.Write) {
		return
	}
	c.log.Info().Str("filename", e.Name).Msg("config changed, reloading")
	oldConfigProxies := maps.Clone(c.configProxies)

	// Delete all entries because it's not deleted when loading from file
	for k := range c.configProxies {
		delete(c.configProxies, k)
	}
	if err := c.file.Load(); err != nil {
		c.log.Error().Err(err).Msg("error loading config")
	}

	// delete proxies that don't exist in new config
	for name := range oldConfigProxies {
		if _, ok := c.configProxies[name]; !ok {
			c.eventsChan <- targetproviders.TargetEvent{
				ID:             name,
				TargetProvider: c,
				Action:         targetproviders.ActionStopProxy,
			}
		}
	}

	for name := range c.configProxies {
		// start new proxies
		if _, ok := oldConfigProxies[name]; !ok {
			c.eventsChan <- targetproviders.TargetEvent{
				ID:             name,
				TargetProvider: c,
				Action:         targetproviders.ActionStartProxy,
			}
			continue
		}
		// restart if the proxy configuration changed
		//
		if !reflect.DeepEqual(c.configProxies[name], oldConfigProxies[name]) {
			c.eventsChan <- targetproviders.TargetEvent{
				ID:             name,
				TargetProvider: c,
				Action:         targetproviders.ActionRestartProxy,
			}
		}
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
		port, err := model.NewPortShortLabel(k)
		if err != nil {
			c.log.Error().Err(err).Str("port", k).Msg("error creating port config")
		}

		port.IsRedirect = v.IsRedirect

		for _, target := range v.Targets {
			targetURL, err := url.Parse(target)
			if err != nil || targetURL.Scheme == "" || targetURL.Host == "" {
				c.log.Error().Err(err).Str("port", k).Str("targetUrl", target).Msg("Invalid target URL")
				// don't add this port and continue with other targets
				continue
			}

			port.AddTarget(targetURL)
		}

		if len(port.GetTargets()) == 0 {
			c.log.Error().Str("port", k).Msg("no targets found for port")
			continue
		}

		port.TLSValidate = v.TLSValidate
		port.Tailscale = v.Tailscale

		ports[k] = port
	}
	return ports
}
