// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package docker

import (
	"context"
	"fmt"
	"strings"
	"sync"

	ctypes "github.com/docker/docker/api/types/container"
	devents "github.com/docker/docker/api/types/events"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/client"
	"github.com/rs/zerolog"

	"github.com/almeidapaulopt/tsdproxy/internal/config"
	"github.com/almeidapaulopt/tsdproxy/internal/model"
	"github.com/almeidapaulopt/tsdproxy/internal/targetproviders"
)

type (
	// Client struct implements TargetProvider
	Client struct {
		docker                   *client.Client
		log                      zerolog.Logger
		containers               map[string]*container
		name                     string
		host                     string
		defaultTargetHostname    string
		defaultProxyProvider     string
		defaultBridgeAdress      string
		tryDockerInternalNetwork bool

		mutex sync.Mutex
	}
)

var _ targetproviders.TargetProvider = (*Client)(nil)

// New function returns a new Docker TargetProvider
func New(log zerolog.Logger, name string, provider *config.DockerTargetProviderConfig) (*Client, error) {
	newlog := log.With().Str("docker", name).Logger()
	newlog.Trace().Msg("New Docker TargetProvider")
	defer newlog.Trace().Msg("End New Docker TargetProvider")

	docker, err := client.NewClientWithOpts(
		client.WithHost(provider.Host),
		client.WithAPIVersionNegotiation())
	if err != nil {
		log.Error().Err(err).Msg("Error creating Docker client")
		return nil, err
	}

	c := &Client{
		docker:                   docker,
		log:                      newlog,
		name:                     name,
		host:                     provider.Host,
		defaultTargetHostname:    provider.TargetHostname,
		defaultProxyProvider:     provider.DefaultProxyProvider,
		tryDockerInternalNetwork: provider.TryDockerInternalNetwork,
		containers:               make(map[string]*container),
	}

	c.setDefaultBridgeAddress()
	// c.setIsTsdproxyRunningHere()

	return c, nil
}

// Close method implements TargetProvider Close method.
func (c *Client) Close() {
	c.log.Trace().Msg("Close Docker TargetProvider")
	defer c.log.Trace().Msg("End Close Docker TargetProvider")

	if c.docker != nil {
		c.docker.Close()
	}
}

// AddTarget method implements TargetProvider AddTarget method
func (c *Client) AddTarget(id string) (*model.Config, error) {
	c.log.Trace().Msgf("AddTarget %s", id)
	defer c.log.Trace().Msgf("End AddTarget %s", id)

	ctx := context.Background()

	dcontainer, err := c.docker.ContainerInspect(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("error inspecting container: %w", err)
	}

	var dservice swarm.Service

	if serviceID, ok := dcontainer.Config.Labels["com.docker.swarm.service.id"]; ok {
		dservice, _, _ = c.docker.ServiceInspectWithRaw(ctx, serviceID, swarm.ServiceInspectOptions{})
	}

	return c.newProxyConfig(dcontainer, dservice)
}

// DeleteProxy method implements TargetProvider DeleteProxy method
func (c *Client) DeleteProxy(id string) error {
	c.log.Trace().Msgf("DeleteProxy %s", id)
	defer c.log.Trace().Msgf("End DeleteProxy %s", id)

	if _, ok := c.containers[id]; !ok {
		return fmt.Errorf("container %s not found", id)
	}

	c.deleteContainer(id)

	return nil
}

// GetDefaultProxyProviderName method implements TargetProvider GetDefaultProxyProviderName method
func (c *Client) GetDefaultProxyProviderName() string {
	return c.defaultProxyProvider
}

// WatchEvents method implements TargetProvider WatchEvents method
func (c *Client) WatchEvents(ctx context.Context, eventsChan chan targetproviders.TargetEvent, errChan chan error) {
	c.log.Trace().Msg("WatchEvents")
	defer c.log.Trace().Msg("End WatchEvents")
	// Filter Start/stop events for containers
	//
	eventsFilter := filters.NewArgs()
	eventsFilter.Add("label", LabelIsEnabled)
	eventsFilter.Add("type", string(devents.ContainerEventType))
	eventsFilter.Add("event", string(devents.ActionDie))
	eventsFilter.Add("event", string(devents.ActionStart))

	dockereventsChan, dockererrChan := c.docker.Events(ctx, devents.ListOptions{
		Filters: eventsFilter,
	})

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case devent, ok := <-dockereventsChan:
				if !ok {
					return
				}
				switch devent.Action {
				case devents.ActionStart:
					eventsChan <- c.getStartEvent(devent.Actor.ID)
				case devents.ActionDie:
					eventsChan <- c.getStopEvent(devent.Actor.ID)
				}

			case err, ok := <-dockererrChan:
				if ok {
					errChan <- err
				}
				return
			}
		}
	}()

	go c.startAllProxies(ctx, eventsChan, errChan)
}

func (c *Client) startAllProxies(ctx context.Context, eventsChan chan targetproviders.TargetEvent, errChan chan error) {
	c.log.Trace().Msg("startAllProxies")
	defer c.log.Trace().Msg("End startAllProxies")
	// Filter containers with enable set to true
	//
	containerFilter := filters.NewArgs()
	containerFilter.Add("label", LabelIsEnabled)

	containers, err := c.docker.ContainerList(ctx, ctypes.ListOptions{
		Filters: containerFilter,
		All:     false,
	})
	if err != nil {
		errChan <- fmt.Errorf("error listing containers: %w", err)
		return
	}

	for _, container := range containers {
		eventsChan <- c.getStartEvent(container.ID)
	}
}

// newProxyConfig method returns a new proxyconfig.Config
func (c *Client) newProxyConfig(dcontainer ctypes.InspectResponse, dservice swarm.Service) (*model.Config, error) {
	c.log.Trace().Msg("newProxyConfig")
	defer c.log.Trace().Msg("End newProxyConfig")

	ctn := newContainer(c.log, dcontainer, dservice, c.tryDockerInternalNetwork,
		withDefaultBridgeAddress(c.defaultBridgeAdress),
		withDefaultTargetHostname(c.defaultTargetHostname),
		withTargetProviderName(c.name),
	)

	pcfg, err := ctn.newProxyConfig()
	if err != nil {
		return nil, fmt.Errorf("error getting proxy config: %w", err)
	}
	c.addContainer(ctn, ctn.id)
	return pcfg, nil
}

// getStartEvent method returns a targetproviders.TargetEvent for a container start
func (c *Client) getStartEvent(id string) targetproviders.TargetEvent {
	c.log.Trace().Msgf("getStartEvent %s", id)
	defer c.log.Trace().Msgf("End getStartEvent %s", id)

	c.log.Info().Msgf("Container %s started", id)

	return targetproviders.TargetEvent{
		TargetProvider: c,
		ID:             id,
		Action:         targetproviders.ActionStartProxy,
	}
}

// getStopEvent method returns a targetproviders.TargetEvent for a container stop
func (c *Client) getStopEvent(id string) targetproviders.TargetEvent {
	c.log.Trace().Msgf("getStopEvent %s", id)
	defer c.log.Trace().Msgf("End getStopEvent %s", id)

	c.log.Info().Msgf("Container %s stopped", id)

	return targetproviders.TargetEvent{
		TargetProvider: c,
		ID:             id,
		Action:         targetproviders.ActionStopProxy,
	}
}

// addContainer method addContainer the containers map
func (c *Client) addContainer(cont *container, name string) {
	c.log.Trace().Msgf("addContainer %s", name)
	defer c.log.Trace().Msgf("End addContainer %s", name)

	c.mutex.Lock()
	defer c.mutex.Unlock()

	c.containers[name] = cont
}

// deleteContainer method deletes a container from the containers map
func (c *Client) deleteContainer(name string) {
	c.log.Trace().Msgf("deleteContainer %s", name)
	defer c.log.Trace().Msgf("End deleteContainer %s", name)

	c.mutex.Lock()
	defer c.mutex.Unlock()

	delete(c.containers, name)
}

// setDefaultBridgeAddress method returns the default bridge network address
func (c *Client) setDefaultBridgeAddress() {
	c.log.Trace().Msg("getDefaultBridgeAddress")
	defer c.log.Trace().Msg("End getDefaultBridgeAddress")

	filter := filters.NewArgs()
	networks, err := c.docker.NetworkList(context.Background(), network.ListOptions{
		Filters: filter,
	})
	if err != nil {
		c.log.Error().Err(err).Msg("Error listing Docker networks")
		return
	}

	for _, network := range networks {
		if network.Options["com.docker.network.bridge.default_bridge"] == "true" {
			if len(network.IPAM.Config) == 0 {
				c.log.Warn().Str("network", network.Name).Msg("default bridge network has no IPAM config")
				continue
			}
			c.log.Info().Str("defaultIPAdress", network.IPAM.Config[0].Gateway).Msg("Default Network found")

			c.defaultBridgeAdress = strings.TrimSpace(network.IPAM.Config[0].Gateway)
			return
		}
	}
}
