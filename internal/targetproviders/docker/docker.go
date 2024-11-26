// SPDX-FileCopyrightText: 2024 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT
package docker

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/docker/docker/api/types"
	ctypes "github.com/docker/docker/api/types/container"
	devents "github.com/docker/docker/api/types/events"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/rs/zerolog"

	"github.com/almeidapaulopt/tsdproxy/internal/config"
	"github.com/almeidapaulopt/tsdproxy/internal/proxyconfig"
	"github.com/almeidapaulopt/tsdproxy/internal/targetproviders"
)

type (
	// Client struct implements TargetProvider
	Client struct {
		docker                *client.Client
		log                   zerolog.Logger
		containers            map[string]*container
		name                  string
		host                  string
		defaultTargetHostname string
		defaultProxyProvider  string
		defaultBridgeAdress   string

		mutex sync.Mutex
	}
)

// New function returns a new Docker TargetProvider
func New(log zerolog.Logger, name string, provider *config.DockerTargetProviderConfig) (*Client, error) {
	newlog := log.With().Str("docker", name).Logger()

	docker, err := client.NewClientWithOpts(
		client.WithHost(provider.Host),
		client.WithAPIVersionNegotiation())
	if err != nil {
		log.Error().Err(err).Msg("Error creating Docker client")
		return nil, err
	}

	c := &Client{
		docker:                docker,
		log:                   newlog,
		name:                  name,
		host:                  provider.Host,
		defaultTargetHostname: provider.TargetHostname,
		defaultProxyProvider:  provider.DefaultProxyProvider,
		containers:            make(map[string]*container),
	}

	var addr string

	if addr, err = c.getDefaultBridgeAddress(); err != nil {
		return nil, err
	}

	c.defaultBridgeAdress = strings.TrimSpace(addr)

	return c, nil
}

// Close method implements TargetProvider Close method.
func (c *Client) Close() {
	if c.docker != nil {
		c.docker.Close()
	}
}

// GetAllProxies implements TargetProvider GetAllProxies method.
func (c *Client) GetAllProxies() (map[string]*proxyconfig.Config, error) {
	ctx := context.Background()
	proxies := map[string]*proxyconfig.Config{}
	// Filter containers with enable set to true
	//
	containerFilter := filters.NewArgs()
	containerFilter.Add("label", LabelIsEnabled)

	containers, err := c.docker.ContainerList(ctx, ctypes.ListOptions{
		Filters: containerFilter,
		All:     false,
	})
	if err != nil {
		return nil, fmt.Errorf("error listing containers: %w", err)
	}

	for _, container := range containers {
		ctn, err := c.docker.ContainerInspect(ctx, container.ID)
		if err != nil {
			c.log.Error().Err(err).Str("containerID", container.ID).Msg("error inspecting container")
		} else {
			pcfg, err := c.newProxyConfig(ctn)
			if err == nil {
				proxies[pcfg.Hostname] = pcfg
			} else {
				c.log.Error().Err(err).Msg("error initializing proxy for container")
			}
		}
	}

	return proxies, nil
}

// newProxyConfig method returns a new proxyconfig.Config
func (c *Client) newProxyConfig(dcontainer types.ContainerJSON) (*proxyconfig.Config, error) {
	ctn := newContainer(c.log, dcontainer, c.name, c.defaultBridgeAdress, c.defaultTargetHostname)

	pcfg, err := ctn.newProxyConfig()
	if err != nil {
		return nil, fmt.Errorf("error getting proxy config: %w", err)
	}
	c.addContainer(ctn, ctn.container.ID)
	return pcfg, nil
}

// AddTarget method implements TargetProvider AddTarget method
func (c *Client) AddTarget(id string) (*proxyconfig.Config, error) {
	ctx := context.Background()
	dcontainer, err := c.docker.ContainerInspect(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("error inspecting container: %w", err)
	}

	return c.newProxyConfig(dcontainer)
}

// DeleteProxy method implements TargetProvider DeleteProxy method
func (c *Client) DeleteProxy(id string) error {
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
			case devent := <-dockereventsChan:

				switch devent.Action {
				case devents.ActionStart:
					eventsChan <- c.getStartEvent(devent.Actor.ID)
				case devents.ActionDie:
					eventsChan <- c.getStopEvent(devent.Actor.ID)
				}

			case err := <-dockererrChan:
				errChan <- err
			}
		}
	}()
}

// getStartEvent method returns a targetproviders.TargetEvent for a container start
func (c *Client) getStartEvent(id string) targetproviders.TargetEvent {
	c.log.Info().Msgf("Container %s started", id)

	return targetproviders.TargetEvent{
		TargetProvider: c,
		ID:             id,
		Action:         targetproviders.ActionStart,
	}
}

// getStopEvent method returns a targetproviders.TargetEvent for a container stop
func (c *Client) getStopEvent(id string) targetproviders.TargetEvent {
	c.log.Info().Msgf("Container %s stopped", id)
	return targetproviders.TargetEvent{
		TargetProvider: c,
		ID:             id,
		Action:         targetproviders.ActionStop,
	}
}

// addContainer method addContainer the containers map
func (c *Client) addContainer(cont *container, name string) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	c.containers[name] = cont
}

// deleteContainer method deletes a container from the containers map
func (c *Client) deleteContainer(name string) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	delete(c.containers, name)
}

// getDefaultBridgeAddress method returns the default bridge network address
func (c *Client) getDefaultBridgeAddress() (string, error) {
	filter := filters.NewArgs()
	networks, err := c.docker.NetworkList(context.Background(), network.ListOptions{
		Filters: filter,
	})
	if err != nil {
		c.log.Error().Err(err).Msg("Error listing Docker networks")
		return "", err
	}

	for _, network := range networks {
		if network.Options["com.docker.network.bridge.default_bridge"] == "true" {
			c.log.Info().Str("defaultIPAdress", network.IPAM.Config[0].Gateway).Msg("Default Network found")

			return network.IPAM.Config[0].Gateway, nil
		}
	}

	return "", errors.New("could not find default bridge network")
}
