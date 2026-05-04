// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package proxymanager

import (
	"context"
	"errors"
	"maps"
	"sync"

	"github.com/rs/zerolog"

	"github.com/almeidapaulopt/tsdproxy/internal/config"
	"github.com/almeidapaulopt/tsdproxy/internal/model"
	"github.com/almeidapaulopt/tsdproxy/internal/proxyproviders"
	"github.com/almeidapaulopt/tsdproxy/internal/proxyproviders/tailscale"
	"github.com/almeidapaulopt/tsdproxy/internal/targetproviders"
	"github.com/almeidapaulopt/tsdproxy/internal/targetproviders/docker"
	"github.com/almeidapaulopt/tsdproxy/internal/targetproviders/list"
)

type (
	ProxyList          map[string]*Proxy
	TargetProviderList map[string]targetproviders.TargetProvider
	ProxyProviderList  map[string]proxyproviders.Provider

	// ProxyManager struct stores data that is required to manage all proxies
	ProxyManager struct {
		Proxies ProxyList

		log zerolog.Logger

		TargetProviders TargetProviderList
		ProxyProviders  ProxyProviderList

		statusSubscribers map[chan model.ProxyEvent]struct{}

		mtx      sync.RWMutex
		targetMu sync.Map // map[string]*sync.Mutex — per-target-ID lock for serializing events
		hostMu   sync.Map // map[string]*sync.Mutex — per-hostname lock for serializing proxy replacement
	}
)

var (
	ErrProxyProviderNotFound  = errors.New("proxyProvider not found")
	ErrTargetProviderNotFound = errors.New("targetProvider not found")
)

// NewProxyManager function creates a new ProxyManager.
func NewProxyManager(logger zerolog.Logger) *ProxyManager {
	pm := &ProxyManager{
		Proxies:           make(ProxyList),
		TargetProviders:   make(TargetProviderList),
		ProxyProviders:    make(ProxyProviderList),
		statusSubscribers: make(map[chan model.ProxyEvent]struct{}),
		log:               logger.With().Str("module", "proxymanager").Logger(),
	}

	return pm
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

// StopAllProxies method shuts down all proxies.
func (pm *ProxyManager) StopAllProxies() {
	pm.log.Info().Msg("Shutdown all proxies")

	pm.mtx.RLock()
	ids := make([]string, 0, len(pm.Proxies))
	for id := range pm.Proxies {
		ids = append(ids, id)
	}
	pm.mtx.RUnlock()

	var wg sync.WaitGroup
	for _, id := range ids {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			pm.removeProxy(id)
		}(id)
	}
	wg.Wait()
}

// WatchEvents method watches for events from all target providers.
func (pm *ProxyManager) WatchEvents() {
	for _, provider := range pm.TargetProviders {
		go func(provider targetproviders.TargetProvider) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			eventsChan := make(chan targetproviders.TargetEvent)
			errChan := make(chan error)

			go provider.WatchEvents(ctx, eventsChan, errChan)

			for {
				select {
				case event, ok := <-eventsChan:
					if !ok {
						return
					}
					go pm.HandleProxyEvent(event)
				case err, ok := <-errChan:
					if ok {
						pm.log.Err(err).Msg("Error watching events")
					}
					return
				}
			}
		}(provider)
	}
}

// HandleProxyEvent method handles events from a targetprovider.
// Each event is serialized per target ID so that stop/start for the same
// target cannot interleave, while different targets process in parallel.
func (pm *ProxyManager) HandleProxyEvent(event targetproviders.TargetEvent) {
	mu := pm.getTargetLock(event.ID)
	mu.Lock()
	defer mu.Unlock()

	switch event.Action {
	case targetproviders.ActionStartProxy:
		pm.eventStart(event)
	case targetproviders.ActionStopProxy:
		pm.eventStop(event)
	case targetproviders.ActionRestartProxy:
		pm.eventStop(event)
		pm.eventStart(event)
	}
}

// getTargetLock returns a per-target-ID mutex, creating one if needed.
func (pm *ProxyManager) getTargetLock(targetID string) *sync.Mutex {
	v, _ := pm.targetMu.LoadOrStore(targetID, &sync.Mutex{})
	return v.(*sync.Mutex)
}

// SubscribeStatusEvents return a channel of proxy events.
// This events are sent by Proxies and Ports.
func (pm *ProxyManager) SubscribeStatusEvents() <-chan model.ProxyEvent {
	ch := make(chan model.ProxyEvent)

	pm.mtx.Lock()
	pm.statusSubscribers[ch] = struct{}{}
	pm.mtx.Unlock()

	return ch
}

// UnsubscribeStatusEvents remove the channel subscrived in SubscribeStatusEvents
func (pm *ProxyManager) UnsubscribeStatusEvents(ch chan model.ProxyEvent) {
	pm.mtx.Lock()
	delete(pm.statusSubscribers, ch)
	close(ch)
	pm.mtx.Unlock()
}

func (pm *ProxyManager) GetProxies() ProxyList {
	pm.mtx.RLock()
	defer pm.mtx.RUnlock()

	return maps.Clone(pm.Proxies)
}

func (pm *ProxyManager) GetProxy(name string) (*Proxy, bool) {
	pm.mtx.RLock()
	defer pm.mtx.RUnlock()

	proxy, ok := pm.Proxies[name]

	return proxy, ok
}

// broadcastStatusEvents broadcasts proxy status event to all SubscribeStatusEvents
func (pm *ProxyManager) broadcastStatusEvents(event model.ProxyEvent) {
	pm.mtx.RLock()
	for ch := range pm.statusSubscribers {
		select {
		case ch <- event:
		default:
		}
	}
	pm.mtx.RUnlock()
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
	for name, file := range config.Config.Lists {
		p, err := list.New(pm.log, name, file)
		if err != nil {
			pm.log.Error().Err(err).Msg("Error creating Files provider")
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

// addTargetProvider method adds a TargetProvider to the ProxyManager.
func (pm *ProxyManager) addTargetProvider(provider targetproviders.TargetProvider, name string) {
	pm.mtx.Lock()
	defer pm.mtx.Unlock()

	pm.TargetProviders[name] = provider
}

// addProxyProvider method adds	a ProxyProvider to the ProxyManager.
func (pm *ProxyManager) addProxyProvider(provider proxyproviders.Provider, name string) {
	pm.mtx.Lock()
	defer pm.mtx.Unlock()

	pm.ProxyProviders[name] = provider
}

// closeAndRemoveProxy closes and removes any proxy with the given hostname.
func (pm *ProxyManager) closeAndRemoveProxy(hostname string) {
	pm.mtx.Lock()
	old, exists := pm.Proxies[hostname]
	if exists {
		delete(pm.Proxies, hostname)
	}
	pm.mtx.Unlock()

	if old != nil {
		old.Close()
		pm.log.Debug().Str("proxy", hostname).Msg("Closed existing proxy for replacement")
	}
}

// getHostLock returns a per-hostname mutex, creating one if needed.
func (pm *ProxyManager) getHostLock(hostname string) *sync.Mutex {
	v, _ := pm.hostMu.LoadOrStore(hostname, &sync.Mutex{})
	return v.(*sync.Mutex)
}

// removeProxy method removes a Proxy from the ProxyManager.
func (pm *ProxyManager) removeProxy(hostname string) {
	pm.mtx.Lock()
	proxy, exists := pm.Proxies[hostname]
	if !exists {
		pm.mtx.Unlock()
		return
	}

	delete(pm.Proxies, hostname)
	pm.mtx.Unlock()

	proxy.Close()

	pm.log.Debug().Str("proxy", hostname).Msg("Removed proxy")
}

// eventStart method starts a Proxy from a event trigger
func (pm *ProxyManager) eventStart(event targetproviders.TargetEvent) {
	pm.log.Debug().Str("targetID", event.ID).Msg("Adding target")

	pcfg, err := event.TargetProvider.AddTarget(event.ID)
	if err != nil {
		pm.log.Error().Err(err).Str("targetID", event.ID).Msg("Error adding target")
		return
	}

	pm.newAndStartProxy(pcfg.Hostname, pcfg)
}

// eventStop method stops a Proxy from a event trigger
func (pm *ProxyManager) eventStop(event targetproviders.TargetEvent) {
	pm.log.Debug().Str("targetID", event.ID).Msg("Stopping target")

	pm.mtx.Lock()
	var proxy *Proxy
	for _, p := range pm.Proxies {
		if p.Config.TargetID == event.ID {
			proxy = p
			delete(pm.Proxies, p.Config.Hostname)
			break
		}
	}
	pm.mtx.Unlock()

	// Always clean up provider-side state, even if the proxy was already
	// removed from the map by a concurrent addProxy with the same hostname.
	if err := event.TargetProvider.DeleteProxy(event.ID); err != nil {
		pm.log.Debug().Err(err).Str("targetID", event.ID).Msg("Provider cleanup skipped")
	}

	if proxy != nil {
		proxy.Close()
		pm.log.Debug().Str("proxy", proxy.Config.Hostname).Msg("Removed proxy")
	}
}

// newAndStartProxy method creates a new proxy and starts it.
func (pm *ProxyManager) newAndStartProxy(name string, proxyConfig *model.Config) {
	pm.log.Debug().Str("proxy", name).Msg("Creating proxy")

	// Serialize per-hostname so that concurrent starts for the same hostname
	// (from different target IDs) cannot race. The lock covers close-old →
	// create → install → start, guaranteeing exactly one live proxy per
	// hostname and preventing an evicted proxy from starting.
	hmu := pm.getHostLock(proxyConfig.Hostname)
	hmu.Lock()
	defer hmu.Unlock()

	pm.closeAndRemoveProxy(proxyConfig.Hostname)

	proxyProvider, err := pm.getProxyProvider(proxyConfig)
	if err != nil {
		pm.log.Error().Err(err).Msg("Error to get ProxyProvider")
		return
	}

	p, err := NewProxy(pm.log, proxyConfig, proxyProvider)
	if err != nil {
		pm.log.Error().Err(err).Msg("Error creating proxy")
		return
	}

	p.onUpdate = func(event model.ProxyEvent) {
		pm.broadcastStatusEvents(event)
	}

	pm.mtx.Lock()
	pm.Proxies[proxyConfig.Hostname] = p
	pm.mtx.Unlock()

	// broadcasts ProxyStatusInitializing
	pm.broadcastStatusEvents(model.ProxyEvent{
		ID:     p.Config.Hostname,
		Status: model.ProxyStatusInitializing,
	})

	p.Start()
}

// getProxyProvider method returns a ProxyProvider.
func (pm *ProxyManager) getProxyProvider(proxy *model.Config) (proxyproviders.Provider, error) {
	pm.mtx.RLock()
	defer pm.mtx.RUnlock()

	// return ProxyProvider defined in configurtion
	//
	if proxy.ProxyProvider != "" {
		p, ok := pm.ProxyProviders[proxy.ProxyProvider]
		if !ok {
			return nil, ErrProxyProviderNotFound
		}
		return p, nil
	}

	// return default ProxyProvider defined in TargetProvider
	targetProvider, ok := pm.TargetProviders[proxy.TargetProvider]
	if !ok {
		return nil, ErrTargetProviderNotFound
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
	return nil, ErrProxyProviderNotFound
}
