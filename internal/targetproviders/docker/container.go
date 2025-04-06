// SPDX-FileCopyrightText: 2025 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package docker

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/almeidapaulopt/tsdproxy/internal/model"
	"github.com/almeidapaulopt/tsdproxy/web"

	ctypes "github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/swarm"
	"github.com/rs/zerolog"
)

// container struct stores the data from the docker container.
type (
	container struct {
		log                   zerolog.Logger
		ports                 map[string]string
		labels                map[string]string
		image                 string
		id                    string
		targetProviderName    string
		name                  string
		hostname              string
		networkMode           ctypes.NetworkMode
		defaultBridgeAddress  string
		defaultTargetHostname string
		ipAddress             []string
		gateways              []string
		autodetect            bool
	}

	ContainerOption func(*container)
)

// newContainer function returns a new container.
func newContainer(logger zerolog.Logger, dcontainer ctypes.InspectResponse, dservice swarm.Service,
	providerAutoDetect bool, opts ...ContainerOption,
) *container {
	//
	newlog := logger.With().Str("container", dcontainer.Name).Logger()
	newlog.Trace().Msg("New Container")
	defer newlog.Trace().Msg("End New Container")

	c := &container{
		log:         newlog,
		id:          dcontainer.ID,
		name:        dcontainer.Name,
		hostname:    dcontainer.Config.Hostname,
		networkMode: dcontainer.HostConfig.NetworkMode,
		image:       dcontainer.Config.Image,
		labels:      dcontainer.Config.Labels,
		ports:       make(map[string]string),
	}

	for _, opt := range opts {
		opt(c)
	}

	c.autodetect = c.getLabelBool(LabelAutoDetect, providerAutoDetect)

	// add ports from container
	c.setContainerPorts(dcontainer, dservice)
	c.setContainerNetwork(dcontainer)

	return c
}

func (c *container) setContainerPorts(dcontainer ctypes.InspectResponse, dservice swarm.Service) {
	c.log.Trace().Msg("start setContainerPorts")
	defer c.log.Trace().Msg("end setContainerPorts")

	for p, _ := range dcontainer.Config.ExposedPorts {
		c.ports[p.Port()] = ""
	}

	if c.networkMode.IsHost() {
		for p := range dcontainer.HostConfig.PortBindings {
			c.ports[p.Port()] = p.Port()
		}
		return
	}

	for p, b := range dcontainer.NetworkSettings.Ports {
		if b != nil {
			c.ports[p.Port()] = b[0].HostPort
		}
	}

	// add ports from service
	for _, b := range dservice.Endpoint.Ports {
		if _, ok := c.ports[strconv.Itoa(int(b.TargetPort))]; ok {
			continue
		}
		c.ports[strconv.Itoa(int(b.TargetPort))] = strconv.Itoa(int(b.PublishedPort))
	}
}

func (c *container) setContainerNetwork(dcontainer ctypes.InspectResponse) {
	c.log.Trace().Msg("start setContainerNetwork")
	defer c.log.Trace().Msg("end setContainerNetwork")

	// add ip addresses and gateways from networks
	for _, network := range dcontainer.NetworkSettings.Networks {
		if network.IPAddress != "" {
			c.ipAddress = append(c.ipAddress, network.IPAddress)
		}
		if network.Gateway != "" {
			c.gateways = append(c.gateways, network.Gateway)
		}
	}
}

// newProxyConfig method returns a new proxyconfig.Config.
func (c *container) newProxyConfig() (*model.Config, error) {
	c.log.Trace().Msg("New ProxyConfig")
	defer c.log.Trace().Msg("End New ProxyConfig")

	// Get the proxy URL
	//
	hostname, err := c.getProxyHostname()
	if err != nil {
		return nil, fmt.Errorf("error parsing Hostname: %w", err)
	}

	// Get the Tailscale configuration
	tailscale, err := c.getTailscaleConfig()
	if err != nil {
		return nil, err
	}

	pcfg, err := model.NewConfig()
	if err != nil {
		return nil, err
	}

	pcfg.TargetID = c.id
	pcfg.Hostname = hostname
	pcfg.TargetProvider = c.targetProviderName
	pcfg.Tailscale = *tailscale
	pcfg.ProxyProvider = c.getLabelString(LabelProxyProvider, model.DefaultProxyProvider)
	pcfg.ProxyAccessLog = c.getLabelBool(LabelContainerAccessLog, model.DefaultProxyAccessLog)
	pcfg.Dashboard.Visible = c.getLabelBool(LabelDashboardVisible, model.DefaultDashboardVisible)
	pcfg.Dashboard.Label = c.getLabelString(LabelDashboardLabel, pcfg.Hostname)

	pcfg.Dashboard.Icon = c.getLabelString(LabelDashboardIcon, "")
	if pcfg.Dashboard.Icon == "" {
		pcfg.Dashboard.Icon = web.GuessIcon(c.image)
	}

	pcfg.Ports = c.getPorts()

	// add port from legacy labels if no port configured
	if len(pcfg.Ports) == 0 {
		if legacyPort, err := c.getLegacyPort(); err == nil {
			pcfg.Ports["legacy"] = legacyPort
		}
	}

	return pcfg, nil
}

func (c *container) getPorts() model.PortConfigList {
	c.log.Trace().Msg("getPorts")
	defer c.log.Trace().Msg("End getPorts")

	ports := make(model.PortConfigList)
	for k, v := range c.labels {
		if !strings.HasPrefix(k, LabelPort) {
			continue
		}

		parts := strings.Split(v, ",")

		port, err := model.NewPortLongLabel(parts[0])
		if err != nil {
			c.log.Error().Err(err).Str("port", k).Msg("error creating port config")
			continue
		}

		for _, v := range parts[1:] {
			v = strings.TrimSpace(v)
			switch v {
			case PortOptionNoTLSValidate:
				port.TLSValidate = false
			case PortOptionTailscaleFunnel:
				port.Tailscale.Funnel = true
			}
		}

		if !port.IsRedirect {
			port, err = c.generateTargetFromFirstTarget(port)
			if err == nil {
				ports[k] = port
			} else {
				c.log.Error().Err(err).Str("port", k).Msg("error generating target")
			}
		}
	}

	return ports
}

func (c *container) generateTargetFromFirstTarget(port model.PortConfig) (model.PortConfig, error) {
	c.log.Trace().Msg("generateTargetFromFirstTarget")
	defer c.log.Trace().Msg("End generateTargetFromFirstTarget")

	// multiple targets not supported in this TargetProvider
	p := port.GetFirstTarget()

	targetURL, err := c.getTargetURL(p)
	if err != nil {
		return port, err
	}
	c.log.Debug().Str("port", port.String()).Str("target", targetURL.String()).Msg("target URL")

	port.ReplaceTarget(p, targetURL)

	return port, nil
}

// getTailscaleConfig method returns the tailscale configuration.
func (c *container) getTailscaleConfig() (*model.Tailscale, error) {
	c.log.Trace().Msg("getTailscaleConfig")
	defer c.log.Trace().Msg("End getTailscaleConfig")

	authKey := c.getLabelString(LabelAuthKey, "")

	authKey, err := c.getAuthKeyFromAuthFile(authKey)
	if err != nil {
		return nil, fmt.Errorf("error setting auth key from file : %w", err)
	}

	tags := c.getLabelString(LabelTags, "")

	return &model.Tailscale{
		Ephemeral:    c.getLabelBool(LabelEphemeral, model.DefaultTailscaleEphemeral),
		RunWebClient: c.getLabelBool(LabelRunWebClient, model.DefaultTailscaleRunWebClient),
		Verbose:      c.getLabelBool(LabelTsnetVerbose, model.DefaultTailscaleVerbose),
		AuthKey:      authKey,
		Tags:         tags,
	}, nil
}

// getName method returns the name of the container
func (c *container) getName() string {
	return strings.TrimLeft(c.name, "/")
}

// getTargetURL method returns the container target URL
func (c *container) getTargetURL(iPort *url.URL) (*url.URL, error) {
	c.log.Trace().Msg("getTargetURL")
	defer c.log.Trace().Msg("End getTargetURL")

	internalPort := iPort.Port()
	publishedPort := c.getPublishedPort(internalPort)

	if internalPort == "" && publishedPort == "" {
		return nil, ErrNoPortFoundInContainer
	}

	// return localhost if container same as host to serve the dashboard
	if osname, err := os.Hostname(); err == nil && strings.HasPrefix(c.id, osname) {
		return url.Parse("http://127.0.0.1:" + internalPort)
	}

	// set autodetect
	if c.autodetect {
		// repeat auto detect in case the container is not ready
		for try := range autoDetectTries {
			c.log.Info().Int("try", try).Msg("Trying to auto detect target URL")
			if port, err := c.tryConnectContainer(iPort.Scheme, internalPort, publishedPort); err == nil {
				return port, nil
			}
			// wait to container get ready in case of startup
			time.Sleep(autoDetectSleep)
		}
	}

	if c.networkMode == "host" && c.defaultBridgeAddress != "" {
		return url.Parse(iPort.Scheme + "://" + c.defaultTargetHostname + ":" + internalPort)
	}

	// auto detect failed or disabled, use published port
	if publishedPort == "" {
		return nil, ErrNoPortFoundInContainer
	}

	return url.Parse(iPort.Scheme + "://" + c.defaultTargetHostname + ":" + publishedPort)
}

// getPublishedPort method returns the container port
func (c *container) getPublishedPort(internalPort string) string {
	c.log.Trace().Msg("getPublishedPort")
	defer c.log.Trace().Msg("End getPublishedPort")

	for internal, published := range c.ports {
		if internal == internalPort {
			return published
		}
	}

	return ""
}

// getProxyHostname method returns the proxy URL from the container label.
func (c *container) getProxyHostname() (string, error) {
	c.log.Trace().Msg("getProxyHostname")
	defer c.log.Trace().Msg("End getProxyHostname")

	// Set custom proxy URL if present the Label in the container
	if customName, ok := c.labels[LabelName]; ok {
		// validate url
		if _, err := url.Parse("https://" + customName); err != nil {
			return "", err
		}
		return customName, nil
	}

	return c.getName(), nil
}

func withTargetProviderName(name string) ContainerOption {
	return func(c *container) {
		c.targetProviderName = name
	}
}

func withDefaultBridgeAddress(address string) ContainerOption {
	return func(c *container) {
		c.defaultBridgeAddress = address
	}
}

func withDefaultTargetHostname(hostname string) ContainerOption {
	return func(c *container) {
		c.defaultTargetHostname = hostname
	}
}
