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
	LabelURL           = LabelPrefix + "url"
	LabelContainerPort = LabelPrefix + "container_port"
)

type Container struct {
	Info types.ContainerJSON
	ID   string
}

func NewContainer(ctx context.Context, containerID string, docker *client.Client) (*Container, error) {
	// Get the container info
	containerInfo, err := docker.ContainerInspect(ctx, containerID)
	if err != nil {
		return nil, fmt.Errorf("error inspecting container: %w", err)
	}

	return &Container{
		Info: containerInfo,
		ID:   containerID,
	}, nil
}

func (c *Container) GetName() string {
	return strings.TrimLeft(c.Info.Name, "/")
}

func (c *Container) GetDefaultPort() string {
	for port := range c.Info.HostConfig.PortBindings {
		return port.Port()
	}

	// if no port found, default to 80
	return "80"
}

func (c *Container) GetIP() string {
	for _, net := range c.Info.NetworkSettings.Networks {
		return net.IPAddress
	}

	// if no port found, default to 80
	return "80"
}

func (c *Container) GetTargetURL() (*url.URL, error) {
	// Set default proxy URL (virtual server in Tailscale)

	containerPort := c.GetDefaultPort()

	// If Label is defined, get the container port
	//
	if customContainerPort, ok := c.Info.Config.Labels[LabelContainerPort]; ok {
		containerPort = customContainerPort
	}

	ip := c.GetIP()

	return url.Parse(fmt.Sprintf("http://%s:%s", ip, containerPort))
}

func (c *Container) GetProxyURL() (*url.URL, error) {
	// set default proxy URL
	proxyURL, _ := url.Parse(fmt.Sprintf("https://%s:%s", c.GetName(), "443"))

	// Set custom proxy URL if present the Label in the container
	if customURL, ok := c.Info.Config.Labels[LabelURL]; ok {
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
