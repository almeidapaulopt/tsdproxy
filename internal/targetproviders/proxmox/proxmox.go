// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package proxmox

import (
	"context"
	"fmt"
	"reflect"
	"strconv"
	"sync"
	"time"

	"github.com/rs/zerolog"

	"github.com/almeidapaulopt/tsdproxy/internal/config"
	"github.com/almeidapaulopt/tsdproxy/internal/core/httpclient"
	"github.com/almeidapaulopt/tsdproxy/internal/model"
	"github.com/almeidapaulopt/tsdproxy/internal/targetproviders"
	"github.com/almeidapaulopt/tsdproxy/web"
)

type (
	// Client struct implements TargetProvider.
	Client struct {
		log                   zerolog.Logger
		api                   APIClient
		assets                *web.Assets
		guests                map[string]*guest
		states                map[string]guestState
		name                  string
		defaultTargetHostname string
		defaultProxyProvider  string
		pollPeriod            time.Duration
		mutex                 sync.Mutex

		healthCheckInterval     int
		healthCheckFailures     int
		healthCheckCooldown     int
		rateLimitRPS            int
		rateLimitBurst          int
		healthCheckEnabled      bool
		rateLimitEnabled        bool
		autoRestart             bool
		proxyAccessLogDefault   bool
		allowGuestFunnel        bool
		allowTLSValidateDisable bool
	}

	// guestState is the last poll snapshot of a guest, diffed against the
	// next poll to decide which target events to emit.
	guestState struct {
		notes    *notesConfig
		status   string
		resource GuestResource
	}
)

// enabled reports whether the snapshot's notes enable the guest.
func (s guestState) enabled() bool {
	return s.notes != nil && s.notes.Enable
}

var _ targetproviders.TargetProvider = (*Client)(nil)

// New function returns a new Proxmox TargetProvider. It connects to the
// Proxmox VE API and fails fast on unreachable hosts or invalid tokens.
// An optional httpclient.Doer overrides the default HTTP client.
func New(log zerolog.Logger, name string, provider *config.ProxmoxTargetProviderConfig,
	proxyAccessLogDefault bool, assets *web.Assets, doers ...httpclient.Doer,
) (*Client, error) {
	newlog := log.With().Str("proxmox", name).Logger()
	newlog.Trace().Msg("New Proxmox TargetProvider")
	defer newlog.Trace().Msg("End New Proxmox TargetProvider")

	api, err := newRestClient(provider, doers...)
	if err != nil {
		return nil, fmt.Errorf("error connecting to Proxmox: %w", err)
	}

	c := &Client{
		api:                     api,
		log:                     newlog,
		name:                    name,
		pollPeriod:              time.Duration(provider.PollIntervalSeconds) * time.Second,
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
		allowGuestFunnel:        provider.AllowGuestFunnel,
		allowTLSValidateDisable: provider.AllowTLSValidateDisable,
		guests:                  make(map[string]*guest),
		states:                  make(map[string]guestState),
	}

	return c, nil
}

// Close method implements TargetProvider Close method. It releases the REST
// client's idle connections.
func (c *Client) Close() {
	c.log.Trace().Msg("Close Proxmox TargetProvider")
	defer c.log.Trace().Msg("End Close Proxmox TargetProvider")

	c.api.Close()
}

// AddTarget method implements TargetProvider AddTarget method
func (c *Client) AddTarget(id string) (*model.Config, error) {
	c.log.Trace().Msgf("AddTarget %s", id)
	defer c.log.Trace().Msgf("End AddTarget %s", id)

	pcfg, g, err := c.buildProxyConfig(id)
	if err != nil {
		return nil, err
	}

	c.mutex.Lock()
	c.guests[id] = g
	c.mutex.Unlock()

	return pcfg, nil
}

// ReResolve re-fetches the guest and returns a fresh proxy config.
// Unlike AddTarget, it does not track the guest.
func (c *Client) ReResolve(id string) (*model.Config, error) {
	c.log.Trace().Msgf("ReResolve %s", id)
	defer c.log.Trace().Msgf("End ReResolve %s", id)

	pcfg, _, err := c.buildProxyConfig(id)
	if err != nil {
		return nil, err
	}
	return pcfg, nil
}

// buildProxyConfig fetches the guest resource, config and interfaces, and
// builds the per-proxy configuration.
func (c *Client) buildProxyConfig(id string) (*model.Config, *guest, error) {
	ctx, cancel := context.WithTimeout(context.Background(), guestRequestTimeout)
	defer cancel()

	resource, err := c.findGuestResource(ctx, id)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %s", ErrGuestNotFound, id)
	}

	cfg, err := c.api.GetGuestConfig(ctx, resource.Node, resource.Type, strconv.Itoa(resource.VMID))
	if err != nil {
		return nil, nil, fmt.Errorf("error fetching guest config: %w", err)
	}

	notesCfg, err := c.parseGuestNotes(resource, cfg)
	if err != nil {
		return nil, nil, fmt.Errorf("error parsing guest notes: %w", err)
	}

	if !notesCfg.Enable {
		return nil, nil, fmt.Errorf("%w: %s", ErrGuestNotEnabled, id)
	}

	if resource.Status != statusRunning {
		return nil, nil, fmt.Errorf("%w: %s (%s)", ErrGuestNotRunning, id, resource.Status)
	}

	// A missing guest agent (or a failed interfaces call) leaves the guest
	// without addresses; the provider targetHostname fallback still applies.
	interfaces, err := c.api.GetGuestInterfaces(ctx, resource.Node, resource.Type, strconv.Itoa(resource.VMID))
	if err != nil {
		c.log.Debug().Err(err).Str("guest", id).Msg("error fetching guest interfaces")
	}

	g := newGuest(c.log, *resource, cfg, notesCfg, interfaces,
		withDefaultTargetHostname(c.defaultTargetHostname),
		withTargetProviderName(c.name),
		withProviderAutoRestart(c.autoRestart),
		withProviderHealthCheck(c.healthCheckEnabled, c.healthCheckInterval, c.healthCheckFailures, c.healthCheckCooldown),
		withProviderRateLimit(c.rateLimitEnabled, c.rateLimitRPS, c.rateLimitBurst),
		withProxyAccessLogDefault(c.proxyAccessLogDefault),
		withAssets(c.assets),
		withAllowGuestFunnel(c.allowGuestFunnel),
		withAllowTLSValidateDisable(c.allowTLSValidateDisable),
	)

	pcfg, err := g.newProxyConfig(ctx, id)
	if err != nil {
		return nil, nil, fmt.Errorf("error getting proxy config: %w", err)
	}

	return pcfg, g, nil
}

// parseGuestNotes parses the tsdproxy block from the guest
// notes/description field. PVE 9 exposes both QEMU and LXC text as
// "description"; older releases used "notes" for QEMU — accept whichever
// is set.
func (c *Client) parseGuestNotes(_ *GuestResource, cfg *GuestConfig) (*notesConfig, error) {
	if cfg.Description != "" {
		return parseTsdproxyConfig(cfg.Description)
	}
	return parseTsdproxyConfig(cfg.Notes)
}

// findGuestResource locates the cluster resource of a guest ID. The poll
// snapshot is consulted first; a miss triggers a fresh listing (AddTarget
// may race with the poll loop).
func (c *Client) findGuestResource(ctx context.Context, id string) (*GuestResource, error) {
	c.mutex.Lock()
	state, existed := c.states[id]
	c.mutex.Unlock()

	if existed && state.resource.ID == id {
		return &state.resource, nil
	}

	guests, err := c.api.ListGuests(ctx)
	if err != nil {
		return nil, fmt.Errorf("error listing guests: %w", err)
	}

	for i := range guests {
		if guests[i].ID == id {
			return &guests[i], nil
		}
	}

	return nil, ErrGuestNotFound
}

// DeleteProxy method implements TargetProvider DeleteProxy method
func (c *Client) DeleteProxy(id string) error {
	c.log.Trace().Msgf("DeleteProxy %s", id)
	defer c.log.Trace().Msgf("End DeleteProxy %s", id)

	c.mutex.Lock()
	if _, ok := c.guests[id]; !ok {
		c.mutex.Unlock()
		return fmt.Errorf("%w: %s", targetproviders.ErrTargetNotFound, id)
	}
	delete(c.guests, id)
	c.mutex.Unlock()

	return nil
}

// GetDefaultProxyProviderName method implements TargetProvider
// GetDefaultProxyProviderName method
func (c *Client) GetDefaultProxyProviderName() string {
	return c.defaultProxyProvider
}

// WatchEvents method implements TargetProvider WatchEvents method. Proxmox
// VE has no push event API, so guest lifecycle is discovered by polling the
// cluster resources endpoint and diffing consecutive snapshots:
//
//   - new, (re)started or re-enabled guest   → Start
//   - stopped, disabled or deleted guest     → Stop (only when a proxy exists)
//   - tsdproxy settings changed while up     → Restart (Start when untracked)
//
// The first poll doubles as the initial scan. A failure of that poll is
// reported on errChan so the ProxyManager reconnect loop retries with
// backoff; later transient failures keep the watcher (and its snapshot)
// alive until the API answers again.
func (c *Client) WatchEvents(ctx context.Context, eventsChan chan targetproviders.TargetEvent, errChan chan error) {
	c.log.Trace().Msg("WatchEvents")
	defer c.log.Trace().Msg("End WatchEvents")

	ticker := time.NewTicker(c.pollPeriod)
	defer ticker.Stop()

	if err := c.pollOnce(ctx, eventsChan); err != nil {
		select {
		case <-ctx.Done():
		case errChan <- fmt.Errorf("error polling Proxmox cluster resources: %w", err):
		}
		return
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := c.pollOnce(ctx, eventsChan); err != nil {
				c.log.Warn().Err(err).Msg("error polling cluster resources, keeping snapshot")
			}
		}
	}
}

// pollOnce lists the cluster guests and emits target events for the
// differences against the previous snapshot.
func (c *Client) pollOnce(ctx context.Context, eventsChan chan targetproviders.TargetEvent) error {
	c.log.Trace().Msg("pollOnce")
	defer c.log.Trace().Msg("End pollOnce")

	guests, err := c.api.ListGuests(ctx)
	if err != nil {
		return fmt.Errorf("error listing cluster resources: %w", err)
	}

	seen := make(map[string]bool, len(guests))

	for i := range guests {
		resource := guests[i]

		if resource.Template != 0 {
			continue
		}
		if resource.Type != guestTypeQemu && resource.Type != guestTypeLXC {
			continue
		}
		seen[resource.ID] = true

		c.mutex.Lock()
		prev, existed := c.states[resource.ID]
		_, tracked := c.guests[resource.ID]
		c.mutex.Unlock()

		if resource.Status != statusRunning {
			c.handleStoppedGuest(ctx, eventsChan, resource, tracked)
			continue
		}

		c.handleRunningGuest(ctx, eventsChan, resource, prev, existed, tracked)
	}

	c.emitDeleted(ctx, eventsChan, seen)

	return nil
}

// handleStoppedGuest records a non-running guest and tears its proxy down
// when one exists.
func (c *Client) handleStoppedGuest(ctx context.Context, eventsChan chan targetproviders.TargetEvent, resource GuestResource, tracked bool) {
	if tracked {
		c.log.Info().Str("guest", resource.ID).Msg("guest stopped")
		c.sendEvent(ctx, eventsChan, resource.ID, targetproviders.ActionStopProxy)
	}
	c.storeState(resource.ID, guestState{status: resource.Status})
}

// handleRunningGuest fetches a running guest's notes and emits the event
// implied by the diff against its previous snapshot. Fetch or parse
// failures return early and keep the previous snapshot: a failing config
// fetch must not look like a config change or a deletion.
func (c *Client) handleRunningGuest(ctx context.Context, eventsChan chan targetproviders.TargetEvent,
	resource GuestResource, prev guestState, existed, tracked bool,
) {
	cfg, err := c.api.GetGuestConfig(ctx, resource.Node, resource.Type, strconv.Itoa(resource.VMID))
	if err != nil {
		c.log.Debug().Err(err).Str("guest", resource.ID).Msg("error fetching guest config")
		return
	}

	notesCfg, err := c.parseGuestNotes(&resource, cfg)
	if err != nil {
		c.log.Warn().Err(err).Str("guest", resource.ID).Msg("error parsing tsdproxy notes")
		return
	}

	switch {
	case !notesCfg.Enable:
		// Disabled while running: tear the proxy down.
		if tracked {
			c.log.Info().Str("guest", resource.ID).Msg("guest disabled for tsdproxy")
			c.sendEvent(ctx, eventsChan, resource.ID, targetproviders.ActionStopProxy)
		}
	case !existed || prev.status != statusRunning || !prev.enabled():
		// New, restarted or re-enabled guest (also the initial scan).
		c.log.Info().Str("guest", resource.ID).Msg("guest started")
		c.sendEvent(ctx, eventsChan, resource.ID, targetproviders.ActionStartProxy)
	case !reflect.DeepEqual(prev.notes, notesCfg):
		if tracked {
			c.log.Info().Str("guest", resource.ID).Msg("tsdproxy settings changed, restarting proxy")
			c.sendEvent(ctx, eventsChan, resource.ID, targetproviders.ActionRestartProxy)
		} else {
			// Settings changed on a guest whose proxy is gone — retry
			// the start instead of waiting for a guest restart.
			c.log.Info().Str("guest", resource.ID).Msg("tsdproxy settings changed, starting proxy")
			c.sendEvent(ctx, eventsChan, resource.ID, targetproviders.ActionStartProxy)
		}
	}

	c.storeState(resource.ID, guestState{status: statusRunning, notes: notesCfg, resource: resource})
}

// emitDeleted emits Stop for tracked guests that vanished from the cluster
// resources listing, and drops their snapshot.
func (c *Client) emitDeleted(ctx context.Context, eventsChan chan targetproviders.TargetEvent, seen map[string]bool) {
	c.mutex.Lock()
	deleted := make([]string, 0)
	for id := range c.states {
		if !seen[id] {
			deleted = append(deleted, id)
		}
	}
	for _, id := range deleted {
		delete(c.states, id)
	}
	trackedDeleted := make(map[string]bool, len(deleted))
	for _, id := range deleted {
		_, ok := c.guests[id]
		trackedDeleted[id] = ok
	}
	c.mutex.Unlock()

	for id, tracked := range trackedDeleted {
		if !tracked {
			continue
		}
		c.log.Info().Str("guest", id).Msg("guest deleted")
		c.sendEvent(ctx, eventsChan, id, targetproviders.ActionStopProxy)
	}
}

func (c *Client) storeState(id string, state guestState) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	// Preserve the last known resource so AddTarget can resolve node and
	// type after the snapshot was taken.
	if state.resource.ID == "" {
		if prev, ok := c.states[id]; ok {
			state.resource = prev.resource
		}
	}

	c.states[id] = state
}

func (c *Client) sendEvent(ctx context.Context, eventsChan chan targetproviders.TargetEvent, id string, action targetproviders.ActionType) {
	select {
	case <-ctx.Done():
	case eventsChan <- targetproviders.TargetEvent{
		TargetProvider: c,
		ID:             id,
		Action:         action,
	}:
	}
}
