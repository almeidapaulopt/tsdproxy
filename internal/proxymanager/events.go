// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package proxymanager

import (
	"context"
	"fmt"
	"time"

	"github.com/almeidapaulopt/tsdproxy/internal/model"
	"github.com/almeidapaulopt/tsdproxy/internal/targetproviders"
)

// WatchEvents method watches for events from all target providers.
func (pm *ProxyManager) WatchEvents() {
	for _, provider := range pm.TargetProviders {
		pm.eventsWg.Add(1)
		go func(provider targetproviders.TargetProvider) {
			defer pm.eventsWg.Done()
			backoff := time.Second

			for {
				select {
				case <-pm.ctx.Done():
					return
				default:
				}

				ctx, cancel := context.WithCancel(pm.ctx)

				eventsChan := make(chan targetproviders.TargetEvent)
				errChan := make(chan error, 1)

				go provider.WatchEvents(ctx, eventsChan, errChan)

			streamLoop:
				for {
					select {
					case <-pm.ctx.Done():
						cancel()
						return
					case event, ok := <-eventsChan:
						if !ok {
							cancel()
							backoff = pm.reconnectBackoff(provider, backoff, "event stream closed")
							break streamLoop
						}
						pm.dispatchProxyEvent(event)
						backoff = time.Second
					case err, ok := <-errChan:
						cancel()
						msg := "event stream error"
						if ok && err != nil {
							pm.log.Err(err).Str("provider", provider.GetDefaultProxyProviderName()).Msg(msg)
						}
						backoff = pm.reconnectBackoff(provider, backoff, msg)
						break streamLoop
					}
				}
			}
		}(provider)
	}
}

const maxWatchBackoff = 5 * time.Minute

const backoffMultiplier = 2

func (pm *ProxyManager) reconnectBackoff(provider targetproviders.TargetProvider, current time.Duration, reason string) time.Duration {
	pm.log.Warn().Str("provider", provider.GetDefaultProxyProviderName()).
		Dur("retry_after", current).
		Msg(reason + ", reconnecting")
	timer := time.NewTimer(current)
	select {
	case <-pm.ctx.Done():
		if !timer.Stop() {
			<-timer.C
		}
	case <-timer.C:
	}
	next := current * backoffMultiplier
	if next > maxWatchBackoff {
		return maxWatchBackoff
	}
	return next
}

// dispatchProxyEvent spawns a tracked goroutine to handle a target event.
// The goroutine is tracked by eventHandlerWg so StopAllProxies can wait for
// in-flight handlers before tearing down proxies. Without this tracking,
// StopAllProxies could return (or the process could exit) while a handler
// is still mid-cleanup — losing resource teardown or racing with map
// mutations.
//
// The stopping check is a belt-and-suspenders guard: the primary safety
// comes from eventsWg.Wait() preceding eventHandlerWg.Wait() in
// StopAllProxies (so WatchEvents goroutines — the only production caller —
// have already exited). But dispatchProxyEvent is package-visible and tests
// call it directly; the check prevents untracked goroutines if a caller
// invokes it after shutdown begins.
func (pm *ProxyManager) dispatchProxyEvent(event targetproviders.TargetEvent) {
	if pm.stopping.Load() {
		return
	}
	pm.eventHandlerWg.Add(1)
	go func() {
		defer pm.eventHandlerWg.Done()
		pm.HandleProxyEvent(event)
	}()
}

// HandleProxyEvent method handles events from a targetprovider.
// Each event is serialized per target ID so that stop/start for the same
// target cannot interleave, while different targets process in parallel.
//
// Start() runs OUTSIDE the target lock so a blocking Tailscale login does
// not prevent stop events for other targets from being processed.
func (pm *ProxyManager) HandleProxyEvent(event targetproviders.TargetEvent) {
	pm.targetLocks.Lock(event.ID)

	var proxyToStart *Proxy
	var err error

	switch event.Action {
	case targetproviders.ActionStartProxy:
		proxyToStart, err = pm.eventStart(event)
	case targetproviders.ActionStopProxy:
		pm.eventStop(event)
	case targetproviders.ActionRestartProxy:
		pm.eventStop(event)
		proxyToStart, err = pm.eventStart(event)
	default:
		pm.log.Warn().Str("targetID", event.ID).Msgf("unknown proxy event action: %d", event.Action)
	}

	pm.targetLocks.Unlock(event.ID)

	if err != nil {
		pm.log.Error().Err(err).Str("targetID", event.ID).Msg("Error processing proxy event")
		return
	}

	if proxyToStart != nil {
		// Re-check that the proxy is still in the map with the same
		// pointer identity. A concurrent stop event could have removed
		// and closed it between the target lock release and here.
		current, exists := pm.GetProxy(proxyToStart.Config.Hostname)
		if !exists || current != proxyToStart {
			pm.log.Debug().Str("targetID", event.ID).Msg("proxy removed before Start() could execute")
			return
		}

		if startErr := proxyToStart.Start(); startErr != nil {
			pm.log.Error().Err(startErr).Str("targetID", event.ID).Msg("proxy start failed")

			proxyToStart.mtx.Lock()
			proxyToStart.lastError = startErr.Error()
			proxyToStart.mtx.Unlock()

			pm.broadcastStatusEvents(model.ProxyEvent{
				ID:           proxyToStart.Config.Hostname,
				Status:       model.ProxyStatusError,
				ErrorMessage: startErr.Error(),
			})

			pm.closeProxyIfStillCurrent(proxyToStart)
		}
	}
}

// eventStart method starts a Proxy from a event trigger
func (pm *ProxyManager) eventStart(event targetproviders.TargetEvent) (*Proxy, error) {
	pm.log.Debug().Str("targetID", event.ID).Msg("Adding target")

	pcfg, err := event.TargetProvider.AddTarget(event.ID)
	if err != nil {
		return nil, fmt.Errorf("error adding target: %w", err)
	}

	return pm.newProxy(pcfg.Hostname, pcfg)
}

// eventStop method stops a Proxy from a event trigger.
// It acquires the hostname lock BEFORE deleting from the map and re-checks
// identity after acquiring the lock. This prevents a concurrent
// restartProxyLocked from inserting a new proxy whose DNS/TLS resources
// get destroyed by this cleanup.
func (pm *ProxyManager) eventStop(event targetproviders.TargetEvent) {
	pm.log.Debug().Str("targetID", event.ID).Msg("Stopping target")

	pm.proxyMu.RLock()
	hostname := pm.targetIndex[event.ID]
	pm.proxyMu.RUnlock()

	if err := event.TargetProvider.DeleteProxy(event.ID); err != nil {
		pm.log.Debug().Err(err).Str("targetID", event.ID).Msg("Provider cleanup skipped")
	}

	if hostname == "" {
		return
	}

	pm.hostLocks.Lock(hostname)
	defer pm.hostLocks.Unlock(hostname)

	if removed := pm.removeAndTeardown(hostname, func(p *Proxy) bool { return p.Config.TargetID == event.ID }); removed != nil {
		pm.log.Debug().Str("proxy", hostname).Msg("Removed proxy")
	}
}
