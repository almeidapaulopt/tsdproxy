// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package incus

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog"

	incusapi "github.com/lxc/incus/v7/shared/api"

	"github.com/almeidapaulopt/tsdproxy/internal/config"
	"github.com/almeidapaulopt/tsdproxy/internal/model"
	"github.com/almeidapaulopt/tsdproxy/internal/targetproviders"
	"github.com/almeidapaulopt/tsdproxy/web"
)

const instanceRequestTimeout = 30 * time.Second

type (
	// Client struct implements TargetProvider
	Client struct {
		log                   zerolog.Logger
		incus                 APIClient
		listener              EventListener
		instances             map[string]*instance
		assets                *web.Assets
		name                  string
		project               string
		defaultProxyProvider  string
		defaultTargetHostname string

		healthCheckInterval     int
		healthCheckFailures     int
		healthCheckCooldown     int
		rateLimitRPS            int
		rateLimitBurst          int
		mutex                   sync.Mutex
		healthCheckEnabled      bool
		rateLimitEnabled        bool
		autoRestart             bool
		proxyAccessLogDefault   bool
		allowInstanceFunnel     bool
		allowTLSValidateDisable bool
	}
)

var _ targetproviders.TargetProvider = (*Client)(nil)

// New function returns a new Incus TargetProvider
func New(log zerolog.Logger, name string, provider *config.IncusTargetProviderConfig, proxyAccessLogDefault bool, assets *web.Assets) (*Client, error) {
	newlog := log.With().Str("incus", name).Logger()
	newlog.Trace().Msg("New Incus TargetProvider")
	defer newlog.Trace().Msg("End New Incus TargetProvider")

	server, err := connect(provider)
	if err != nil {
		return nil, fmt.Errorf("error connecting to Incus: %w", err)
	}

	c := &Client{
		incus:                   server,
		log:                     newlog,
		name:                    name,
		project:                 provider.Project,
		defaultTargetHostname:   provider.TargetHostname,
		defaultProxyProvider:    provider.DefaultProxyProvider,
		autoRestart:             provider.AutoRestart,
		healthCheckEnabled:      provider.HealthCheckEnabled,
		healthCheckInterval:     provider.HealthCheckInterval,
		healthCheckFailures:     provider.HealthCheckFailures,
		healthCheckCooldown:     provider.HealthCheckCooldown,
		rateLimitEnabled:        provider.RateLimitEnabled,
		rateLimitRPS:            provider.RateLimitRPS,
		rateLimitBurst:          provider.RateLimitBurst,
		proxyAccessLogDefault:   proxyAccessLogDefault,
		assets:                  assets,
		allowInstanceFunnel:     provider.AllowInstanceFunnel,
		allowTLSValidateDisable: provider.AllowTLSValidateDisable,
		instances:               make(map[string]*instance),
	}

	return c, nil
}

// Close method implements TargetProvider Close method. It disconnects the
// event listener and the SDK connection, stopping its background goroutines.
func (c *Client) Close() {
	c.log.Trace().Msg("Close Incus TargetProvider")
	defer c.log.Trace().Msg("End Close Incus TargetProvider")

	c.mutex.Lock()
	listener := c.listener
	c.mutex.Unlock()

	if listener != nil && listener.IsActive() {
		listener.Disconnect()
	}

	c.incus.Disconnect()
}

// AddTarget method implements TargetProvider AddTarget method
func (c *Client) AddTarget(id string) (*model.Config, error) {
	c.log.Trace().Msgf("AddTarget %s", id)
	defer c.log.Trace().Msgf("End AddTarget %s", id)

	pcfg, inst, err := c.buildProxyConfig(id)
	if err != nil {
		return nil, err
	}

	c.addInstance(inst, id)
	return pcfg, nil
}

// ReResolve re-inspects the instance and returns a fresh proxy config.
// Unlike AddTarget, it does not re-register the instance.
func (c *Client) ReResolve(id string) (*model.Config, error) {
	c.log.Trace().Msgf("ReResolve %s", id)
	defer c.log.Trace().Msgf("End ReResolve %s", id)

	pcfg, _, err := c.buildProxyConfig(id)
	if err != nil {
		return nil, err
	}
	return pcfg, nil
}

// buildProxyConfig fetches the instance and its state, and builds the
// per-proxy configuration.
func (c *Client) buildProxyConfig(id string) (*model.Config, *instance, error) {
	ctx, cancel := context.WithTimeout(context.Background(), instanceRequestTimeout)
	defer cancel()

	inst, _, err := c.incus.GetInstance(id)
	if err != nil {
		return nil, nil, fmt.Errorf("error fetching instance: %w", err)
	}

	if !instanceEnabled(inst.Config) {
		return nil, nil, fmt.Errorf("%w: %s", ErrInstanceNotEnabled, id)
	}

	if inst.StatusCode != incusapi.Running {
		return nil, nil, fmt.Errorf("%w: %s (%s)", ErrInstanceNotRunning, id, inst.Status)
	}

	state := c.waitForInstanceState(ctx, id)

	i := newInstance(c.log, inst, state,
		withDefaultTargetHostname(c.defaultTargetHostname),
		withTargetProviderName(c.name),
		withProviderAutoRestart(c.autoRestart),
		withProviderHealthCheck(c.healthCheckEnabled, c.healthCheckInterval, c.healthCheckFailures, c.healthCheckCooldown),
		withProviderRateLimit(c.rateLimitEnabled, c.rateLimitRPS, c.rateLimitBurst),
		withProxyAccessLogDefault(c.proxyAccessLogDefault),
		withAssets(c.assets),
		withAllowInstanceFunnel(c.allowInstanceFunnel),
		withAllowTLSValidateDisable(c.allowTLSValidateDisable),
	)

	pcfg, err := i.newProxyConfig(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("error getting proxy config: %w", err)
	}

	return pcfg, i, nil
}

// waitForInstanceState polls the instance state until it has at least one
// global-scope address, or the tries are exhausted. DHCP and cloud-init may
// not have finished when the instance-started event fires. The last state
// seen is returned even without addresses — target URL resolution falls back
// to the provider targetHostname, or fails with ErrNoAddressFound.
func (c *Client) waitForInstanceState(ctx context.Context, id string) *incusapi.InstanceState {
	var state *incusapi.InstanceState

	for try := range stateWaitTries {
		var err error
		state, _, err = c.incus.GetInstanceState(id)
		if err != nil {
			c.log.Debug().Err(err).Str("instance", id).Int("try", try).Msg("error fetching instance state")
			return nil
		}

		if hasGlobalAddress(state) {
			return state
		}

		c.log.Debug().Str("instance", id).Int("try", try).Msg("waiting for instance network address")

		select {
		case <-ctx.Done():
			return state
		case <-time.After(stateWaitSleep):
		}
	}

	return state
}

func hasGlobalAddress(state *incusapi.InstanceState) bool {
	if state == nil {
		return false
	}
	for _, network := range state.Network {
		for _, address := range network.Addresses {
			if address.Scope == addressScopeGlobal {
				return true
			}
		}
	}
	return false
}

// DeleteProxy method implements TargetProvider DeleteProxy method
func (c *Client) DeleteProxy(id string) error {
	c.log.Trace().Msgf("DeleteProxy %s", id)
	defer c.log.Trace().Msgf("End DeleteProxy %s", id)

	c.mutex.Lock()
	if _, ok := c.instances[id]; !ok {
		c.mutex.Unlock()
		return fmt.Errorf("%w: %s", targetproviders.ErrTargetNotFound, id)
	}
	delete(c.instances, id)
	c.mutex.Unlock()

	return nil
}

// GetDefaultProxyProviderName method implements TargetProvider GetDefaultProxyProviderName method
func (c *Client) GetDefaultProxyProviderName() string {
	return c.defaultProxyProvider
}

// WatchEvents method implements TargetProvider WatchEvents method. It
// subscribes to Incus lifecycle events for the configured project and emits
// target events for instances with user.tsdproxy.enable=true. Incus cannot
// filter events by config key server-side, so each relevant event triggers a
// config fetch to decide whether to emit.
//
// Events are consumed from a single channel (AddChannel) in a serial loop:
// the SDK delivers handler callbacks in unordered goroutines, which could
// invert rapid stop/start sequences for the same instance. The channel
// closes automatically when the listener ends, covering both server-side
// disconnects and explicit Disconnect.
func (c *Client) WatchEvents(ctx context.Context, eventsChan chan targetproviders.TargetEvent, errChan chan error) {
	c.log.Trace().Msg("WatchEvents")
	defer c.log.Trace().Msg("End WatchEvents")

	listener, err := c.incus.GetEventsByType([]string{incusapi.EventTypeLifecycle})
	if err != nil {
		select {
		case <-ctx.Done():
		case errChan <- fmt.Errorf("error subscribing to Incus events: %w", err):
		}
		return
	}

	c.mutex.Lock()
	c.listener = listener
	c.mutex.Unlock()

	eventCh := listener.AddChannel(nil, listenerChannelSize)

	go c.startAllProxies(ctx, eventsChan, errChan)

	go func() {
		for event := range eventCh {
			select {
			case <-ctx.Done():
				listener.Disconnect()
				return
			default:
			}

			c.handleEvent(ctx, event, eventsChan)
		}

		// Channel closed: the listener ended. Signal the consumer so its
		// reconnect loop re-establishes the event stream.
		if ctx.Err() != nil {
			return
		}

		waitErr := listener.Wait()
		if waitErr != nil && !errors.Is(waitErr, context.Canceled) {
			select {
			case <-ctx.Done():
			case errChan <- fmt.Errorf("incus event stream error: %w", waitErr):
			}
			return
		}

		select {
		case <-ctx.Done():
		case errChan <- fmt.Errorf("%w: incus event stream disconnected", targetproviders.ErrStreamDisconnected):
		}
	}()
}

// handleEvent classifies an Incus lifecycle event into a target event.
func (c *Client) handleEvent(ctx context.Context, event incusapi.Event, eventsChan chan targetproviders.TargetEvent) {
	if event.Type != incusapi.EventTypeLifecycle {
		return
	}

	var lifecycle incusapi.EventLifecycle
	if err := json.Unmarshal(event.Metadata, &lifecycle); err != nil {
		c.log.Debug().Err(err).Msg("error decoding lifecycle event")
		return
	}

	if lifecycle.Name == "" {
		return
	}

	switch lifecycle.Action {
	case incusapi.EventLifecycleInstanceStarted:
		c.emitIfEnabled(ctx, eventsChan, lifecycle.Name, targetproviders.ActionStartProxy)
	case incusapi.EventLifecycleInstanceRestarted:
		c.emitIfEnabled(ctx, eventsChan, lifecycle.Name, targetproviders.ActionRestartProxy)
	case incusapi.EventLifecycleInstanceStopped, incusapi.EventLifecycleInstanceShutdown:
		// Instance config persists while stopped, so the enable check works.
		c.emitIfEnabled(ctx, eventsChan, lifecycle.Name, targetproviders.ActionStopProxy)
	case incusapi.EventLifecycleInstancePaused:
		// Frozen instances have all processes suspended and cannot serve
		// traffic: tear the proxy down. Config stays readable while frozen,
		// so the enable check works like the stopped case.
		c.emitIfEnabled(ctx, eventsChan, lifecycle.Name, targetproviders.ActionStopProxy)
	case incusapi.EventLifecycleInstanceResumed:
		// Unfreeze recreates the proxy. This also covers instances that were
		// frozen while tsdproxy was down: the initial scan skips them (not
		// Running) and the resume event is the only re-entry point.
		c.emitIfEnabled(ctx, eventsChan, lifecycle.Name, targetproviders.ActionStartProxy)
	case incusapi.EventLifecycleInstanceDeleted:
		// Config is gone after deletion; only act on tracked instances.
		c.emitIfTracked(ctx, eventsChan, lifecycle.Name, targetproviders.ActionStopProxy)
	case incusapi.EventLifecycleInstanceUpdated:
		c.handleInstanceUpdated(ctx, eventsChan, lifecycle.Name)
	}
}

// emitIfEnabled fetches the instance config and emits the event only when
// tsdproxy is enabled on the instance.
func (c *Client) emitIfEnabled(ctx context.Context, eventsChan chan targetproviders.TargetEvent, name string, action targetproviders.ActionType) {
	inst, _, err := c.incus.GetInstance(name)
	if err != nil {
		// Instance may already be gone (deleted between events) — skip.
		c.log.Debug().Err(err).Str("instance", name).Msg("error fetching instance for event")
		return
	}

	if !instanceEnabled(inst.Config) {
		return
	}

	c.log.Info().Str("instance", name).Int("action", int(action)).Msg("instance event")

	c.sendEvent(ctx, eventsChan, name, action)
}

// emitIfTracked emits the event only when the provider tracks the instance.
func (c *Client) emitIfTracked(ctx context.Context, eventsChan chan targetproviders.TargetEvent, name string, action targetproviders.ActionType) {
	c.mutex.Lock()
	_, tracked := c.instances[name]
	c.mutex.Unlock()

	if !tracked {
		return
	}

	c.log.Info().Str("instance", name).Int("action", int(action)).Msg("instance event")

	c.sendEvent(ctx, eventsChan, name, action)
}

// handleInstanceUpdated reacts to instance config changes: restart tracked
// instances when their user.tsdproxy.* keys changed, start newly enabled
// ones, stop newly disabled ones. Unrelated config churn (volatile.* keys,
// devices) does not restart proxies.
func (c *Client) handleInstanceUpdated(ctx context.Context, eventsChan chan targetproviders.TargetEvent, name string) {
	inst, _, err := c.incus.GetInstance(name)
	if err != nil {
		c.log.Debug().Err(err).Str("instance", name).Msg("error fetching instance for update")
		return
	}

	c.mutex.Lock()
	tracked, wasTracked := c.instances[name]
	c.mutex.Unlock()

	if !instanceEnabled(inst.Config) {
		if wasTracked {
			c.sendEvent(ctx, eventsChan, name, targetproviders.ActionStopProxy)
		}
		return
	}

	if !wasTracked {
		c.sendEvent(ctx, eventsChan, name, targetproviders.ActionStartProxy)
		return
	}

	if tsdproxyConfigChanged(tracked.config, inst.Config) {
		c.log.Info().Str("instance", name).Msg("tsdproxy config changed, restarting proxy")
		c.sendEvent(ctx, eventsChan, name, targetproviders.ActionRestartProxy)
	}
}

// tsdproxyConfigChanged compares the user.tsdproxy.* subset of two instance
// config maps.
func tsdproxyConfigChanged(oldConfig, newConfig map[string]string) bool {
	oldSubset := filterTsdproxyKeys(oldConfig)
	newSubset := filterTsdproxyKeys(newConfig)

	return !reflect.DeepEqual(oldSubset, newSubset)
}

func filterTsdproxyKeys(configMap map[string]string) map[string]string {
	filtered := make(map[string]string)
	for k, v := range configMap {
		if strings.HasPrefix(k, ConfigPrefix) {
			filtered[k] = v
		}
	}
	return filtered
}

func (c *Client) sendEvent(ctx context.Context, eventsChan chan targetproviders.TargetEvent, name string, action targetproviders.ActionType) {
	select {
	case <-ctx.Done():
	case eventsChan <- targetproviders.TargetEvent{
		TargetProvider: c,
		ID:             name,
		Action:         action,
	}:
	}
}

// startAllProxies emits start events for all running, enabled instances —
// the initial scan when WatchEvents begins.
func (c *Client) startAllProxies(ctx context.Context, eventsChan chan targetproviders.TargetEvent, errChan chan error) {
	c.log.Trace().Msg("startAllProxies")
	defer c.log.Trace().Msg("End startAllProxies")

	instances, err := c.incus.GetInstances(incusapi.InstanceTypeAny)
	if err != nil {
		select {
		case <-ctx.Done():
		case errChan <- fmt.Errorf("error listing instances: %w", err):
		}
		return
	}

	for _, inst := range instances {
		if inst.StatusCode != incusapi.Running {
			continue
		}
		if !instanceEnabled(inst.Config) {
			continue
		}

		c.log.Info().Str("instance", inst.Name).Msg("instance started")

		c.sendEvent(ctx, eventsChan, inst.Name, targetproviders.ActionStartProxy)
	}
}

func (c *Client) addInstance(inst *instance, name string) {
	c.log.Trace().Msgf("addInstance %s", name)
	defer c.log.Trace().Msgf("End addInstance %s", name)

	c.mutex.Lock()
	defer c.mutex.Unlock()

	c.instances[name] = inst
}
