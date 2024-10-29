package containers

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/client"
)

const (
	LabelPrefix    = "tsdproxy."
	LabelIsEnabled = LabelEnable + "=true"

	// Container config labels
	LabelEnable        = LabelPrefix + "enable"
	LabelName          = LabelPrefix + "name"
	LabelContainerPort = LabelPrefix + "container_port"
)

type Container struct {
	Info           types.ContainerJSON
	ID             string
	TargetHostname string
}

func NewContainer(ctx context.Context, containerID string, docker *client.Client, hostname string) (*Container, error) {
	// Get the container info
	containerInfo, err := docker.ContainerInspect(ctx, containerID)
	if err != nil {
		return nil, fmt.Errorf("error inspecting container: %w", err)
	}

	container := &Container{
		Info: containerInfo,
		ID:   containerID,
	}

	container.TargetHostname = container.getTargetHostname(hostname)
	return container, nil
}

func (c *Container) GetName() string {
	return strings.TrimLeft(c.Info.Name, "/")
}

func (c *Container) GetPort() (string, bool) {
	// If Label is defined, get the container port
	//
	if customContainerPort, ok := c.Info.Config.Labels[LabelContainerPort]; ok {
		return customContainerPort, true
	}

	for _, bind := range c.Info.NetworkSettings.Ports {
		if len(bind) > 0 {
			return bind[0].HostPort, true
		}
	}

	return "", false
}

func (c *Container) getTargetHostname(hostname string) string {
	// return container IP address if defined
	if len(c.Info.NetworkSettings.IPAddress) > 0 {
		return c.Info.NetworkSettings.IPAddress
	}

	// return hostname defined in the config
	return hostname
}

func (c *Container) GetTargetURL() (*url.URL, error) {
	// Set default proxy URL (virtual server in Tailscale)

	containerPort, ok := c.GetPort()
	if !ok {
		return nil, fmt.Errorf("no port found in container")
	}

	return url.Parse(fmt.Sprintf("http://%s:%s", c.TargetHostname, containerPort))
}

func (c *Container) GetProxyURL() (*url.URL, error) {
	// set default proxy URL
	proxyURL, _ := url.Parse(fmt.Sprintf("https://%s:%s", c.GetName(), "443"))

	// Set custom proxy URL if present the Label in the container
	if customURL, ok := c.Info.Config.Labels[LabelName]; ok {
		var err error
		proxyURL, err = proxyURL.Parse(customURL)
		if err != nil {
			return nil, fmt.Errorf("error parsing hostname: %w", err)
		}
		if proxyURL.Scheme == "" {
			proxyURL.Scheme = "https"
		}
	}

	return proxyURL, nil
}
