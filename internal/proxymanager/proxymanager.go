// SPDX-FileCopyrightText: 2024 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT
package proxymanager

import (
	"context"
	"errors"
	"sync"

	"github.com/rs/zerolog"

	"github.com/almeidapaulopt/tsdproxy/internal/config"
	"github.com/almeidapaulopt/tsdproxy/internal/proxy"
	"github.com/almeidapaulopt/tsdproxy/internal/proxyconfig"
	"github.com/almeidapaulopt/tsdproxy/internal/proxyproviders"
	"github.com/almeidapaulopt/tsdproxy/internal/proxyproviders/tailscale"
	"github.com/almeidapaulopt/tsdproxy/internal/targetproviders"
	"github.com/almeidapaulopt/tsdproxy/internal/targetproviders/docker"
	"github.com/almeidapaulopt/tsdproxy/internal/targetproviders/files"
)

type (
	ProxyList          map[string]*proxy.Proxy
	TargetProviderList map[string]targetproviders.TargetProvider
	ProxyProviderList  map[string]proxyproviders.Provider

	// ProxyManager struct stores data that is required to manage all proxies
	ProxyManager struct {
		Proxies ProxyList

		log zerolog.Logger

		TargetProviders TargetProviderList
		ProxyProviders  ProxyProviderList

		mutex sync.Mutex
	}
)

// NewProxyManager function creates a new ProxyManager.
func NewProxyManager(logger zerolog.Logger) *ProxyManager {
	return &ProxyManager{
		Proxies:         make(ProxyList),
		TargetProviders: make(TargetProviderList),
		ProxyProviders:  make(ProxyProviderList),
		log:             logger.With().Str("module", "proxymanager").Logger(),
	}
}

// addTargetProvider method adds a TargetProvider to the ProxyManager.
func (pm *ProxyManager) addTargetProvider(provider targetproviders.TargetProvider, name string) {
	pm.mutex.Lock()
	defer pm.mutex.Unlock()

	pm.TargetProviders[name] = provider
}

// addProxyProvider method adds	a ProxyProvider to the ProxyManager.
func (pm *ProxyManager) addProxyProvider(provider proxyproviders.Provider, name string) {
	pm.mutex.Lock()
	defer pm.mutex.Unlock()

	pm.ProxyProviders[name] = provider
}

// Start method starts the ProxyManager.
func (pm *ProxyManager) Start() {
	// Add Providers
	pm.addProxyProviders()
	pm.addTargetProviders()

	// Do not start without providers
	if len(pm.ProxyProviders) == 0 {
		pm.log.Error().Msg("No Proxy Providers found")
		return
	}

	if len(pm.TargetProviders) == 0 {
		pm.log.Error().Msg("No Target Providers found")
		return
	}
}

// addTargetProviders method adds TargetProviders from configuration file.
func (pm *ProxyManager) addTargetProviders() {
	for name, provider := range config.Config.Docker {
		p, err := docker.New(pm.log, name, provider)
		if err != nil {
			pm.log.Error().Err(err).Msg("Error creating Docker provider")
			continue
		}

		pm.addTargetProvider(p, name)
	}
	for name, file := range config.Config.Files {
		p, err := files.New(pm.log, name, file)
		if err != nil {
			pm.log.Error().Err(err).Msg("Error creating Docker provider")
			continue
		}

		pm.addTargetProvider(p, name)
	}
}

// addProxyProviders method adds ProxyProviders from configuration file.
func (pm *ProxyManager) addProxyProviders() {
	pm.log.Debug().Msg("Setting up Tailscale Providers")
	// add Tailscale Providers
	for name, provider := range config.Config.Tailscale.Providers {
		if p, err := tailscale.New(pm.log, name, provider); err != nil {
			pm.log.Error().Err(err).Msg("Error creating Tailscale provider")
		} else {
			pm.log.Debug().Str("provider", name).Msg("Created Proxy provider")
			pm.addProxyProvider(p, name)
		}
	}
}

// addProxy method adds a Proxy to the ProxyManager.
func (pm *ProxyManager) addProxy(proxy *proxy.Proxy) {
	pm.mutex.Lock()
	defer pm.mutex.Unlock()

	pm.Proxies[proxy.Config.Hostname] = proxy
}

// removeProxy method removes a Proxy from the ProxyManager.
func (pm *ProxyManager) removeProxy(hostname string) {
	pm.mutex.Lock()
	defer pm.mutex.Unlock()

	proxy, exists := pm.Proxies[hostname]
	if !exists {
		return
	}

	proxy.Close()

	delete(pm.Proxies, hostname)

	pm.log.Info().Str("proxy", hostname).Msg("Removed proxy for container")
}

// StopAllProxies method shuts down all proxies.
func (pm *ProxyManager) StopAllProxies() {
	pm.log.Info().Msg("Shutdown all proxies")

	for id := range pm.Proxies {
		pm.removeProxy(id)
	}
}

// newAndStartProxy method creates a new proxy and starts it.
func (pm *ProxyManager) newAndStartProxy(name string, proxyConfig *proxyconfig.Config, targetproviders targetproviders.TargetProvider) {
	pm.log.Debug().Str("proxy", name).Msg("Creating proxy")

	proxyProvider, err := pm.getProxyProvider(proxyConfig)
	if err != nil {
		pm.log.Error().Err(err).Msg("Error to get ProxyProvider")
		return
	}

	p, err := proxy.NewProxy(pm.log, proxyConfig, proxyProvider, targetproviders)
	if err != nil {
		pm.log.Error().Err(err).Msg("Error creating proxy")
		return
	}

	pm.addProxy(p)
	p.Start()
}

// getProxyProvider method returns a ProxyProvider.
func (pm *ProxyManager) getProxyProvider(proxy *proxyconfig.Config) (proxyproviders.Provider, error) {
	// return ProxyProvider defined in configurtion
	//
	if proxy.ProxyProvider != "" {
		p, ok := pm.ProxyProviders[proxy.ProxyProvider]
		if !ok {
			return nil, errors.New("ProxyProvider not found")
		}
		return p, nil
	}

	// return defaul ProxyProvider defined in TargetProvider
	targetProvider, ok := pm.TargetProviders[proxy.TargetProvider]
	if !ok {
		return nil, errors.New("TargetProvider not found")
	}
	if p, ok := pm.ProxyProviders[targetProvider.GetDefaultProxyProviderName()]; ok {
		return p, nil
	}

	// return default ProxyProvider from global configurtion
	//
	if p, ok := pm.ProxyProviders[config.Config.DefaultProxyProvider]; ok {
		return p, nil
	}

	// return the first ProxyProvider
	//
	return nil, errors.New("proxyprovider not found")
}

// WatchEvents method watches for events from all target providers.
func (pm *ProxyManager) WatchEvents() {
	for _, provider := range pm.TargetProviders {
		go func(provider targetproviders.TargetProvider) {
			ctx := context.Background()

			eventsChan := make(chan targetproviders.TargetEvent)
			errChan := make(chan error)
			defer close(errChan)
			defer close(eventsChan)

			provider.WatchEvents(ctx, eventsChan, errChan)
			for {
				select {
				case event := <-eventsChan:
					pm.HandleContainerEvent(event)
				case err := <-errChan:
					pm.log.Err(err).Msg("Error watching Docker events")
					return
				}
			}
		}(provider)
	}
}

// HandleContainerEvent method handles events from a targetprovider
func (pm *ProxyManager) HandleContainerEvent(event targetproviders.TargetEvent) {
	switch event.Action {
	case targetproviders.ActionStart:
		pm.eventStart(event)
	case targetproviders.ActionStop:
		pm.eventStop(event)
	case targetproviders.ActionRestart:
		pm.eventStop(event)
		pm.eventStart(event)
	}
}

// eventStart method starts a Proxy from a event trigger
func (pm *ProxyManager) eventStart(event targetproviders.TargetEvent) {
	pm.log.Debug().Str("targetID", event.ID).Msg("Adding target")

	pcfg, err := event.TargetProvider.AddTarget(event.ID)
	if err != nil {
		pm.log.Error().Err(err).Str("targetID", event.ID).Msg("Error adding target")
		return
	}

	pm.newAndStartProxy(pcfg.Hostname, pcfg, event.TargetProvider)
}

// eventStop method stops a Proxy from a event trigger
func (pm *ProxyManager) eventStop(event targetproviders.TargetEvent) {
	pm.log.Debug().Str("targetID", event.ID).Msg("Stopping target")

	proxy := pm.getProxyByTargetID(event.ID)
	if proxy == nil {
		pm.log.Error().Int("action", int(event.Action)).Str("target", event.ID).Msg("No proxy found for target")
		return
	}

	targetprovider := pm.TargetProviders[proxy.Config.TargetProvider]
	if err := targetprovider.DeleteProxy(event.ID); err != nil {
		pm.log.Error().Err(err).Msg("No proxy found for target")
		return
	}

	pm.removeProxy(proxy.Config.Hostname)
}

// getProxyByTargetID method returns a Proxy by TargetID.
func (pm *ProxyManager) getProxyByTargetID(targetID string) *proxy.Proxy {
	for _, p := range pm.Proxies {
		if p.Config.TargetID == targetID {
			return p
		}
	}
	return nil
}
