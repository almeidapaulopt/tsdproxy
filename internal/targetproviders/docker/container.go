// SPDX-FileCopyrightText: 2025 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package docker

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/docker/docker/api/types"

	"github.com/rs/zerolog"

	"github.com/almeidapaulopt/tsdproxy/internal/model"
	"github.com/almeidapaulopt/tsdproxy/web"
)

// container struct stores the data from the docker container.
type container struct {
	log                   zerolog.Logger
	container             types.ContainerJSON
	image                 types.ImageInspect
	defaultTargetHostname string
	defaultBridgeAddress  string
	targetProviderName    string
	scheme                string
	autodetect            bool
}

// newContainer function returns a new container.
func newContainer(logger zerolog.Logger, dcontainer types.ContainerJSON, imageInfo types.ImageInspect,
	targetproviderName string, defaultBridgeAddress string, defaultTargetHostname string,
) *container {
	//
	c := &container{
		log:                   logger.With().Str("container", dcontainer.Name).Logger(),
		container:             dcontainer,
		image:                 imageInfo,
		defaultTargetHostname: defaultTargetHostname,
		// defaultBridgeAddress:  defaultBridgeAddress,
		targetProviderName: targetproviderName,
	}

	c.autodetect = c.getLabelBool(LabelAutoDetect, DefaultAutoDetect)
	c.scheme = c.getLabelString(LabelScheme, DefaultTargetScheme)

	return c
}

// newProxyConfig method returns a new proxyconfig.Config.
func (c *container) newProxyConfig() (*model.Config, error) {
	// Get the proxy URL
	//
	proxyURL, err := c.getProxyURL()
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

	pcfg.TargetID = c.container.ID
	pcfg.Hostname = proxyURL.Hostname()
	pcfg.TargetProvider = c.targetProviderName
	pcfg.Tailscale = *tailscale
	pcfg.ProxyProvider = c.getLabelString(LabelProxyProvider, model.DefaultProxyProvider)
	pcfg.ProxyAccessLog = c.getLabelBool(LabelContainerAccessLog, model.DefaultProxyAccessLog)
	pcfg.Dashboard.Visible = c.getLabelBool(LabelDashboardVisible, model.DefaultDashboardVisible)
	pcfg.Dashboard.Label = c.getLabelString(LabelDashboardLabel, pcfg.Hostname)

	pcfg.Dashboard.Icon = c.getLabelString(LabelDashboardIcon, "")
	if pcfg.Dashboard.Icon == "" {
		pcfg.Dashboard.Icon = web.GuessIcon(c.container.Config.Image)
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
	ports := make(model.PortConfigList)
	for k, v := range c.container.Config.Labels {
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
			// multiple targets not supported in this TargetProvider
			p := port.GetFirstTarget()

			targetURL, err := c.getTargetURL(c.defaultTargetHostname, p.Port())
			if err != nil {
				c.log.Error().Err(err).Msg("error parsing target hostname")
				return ports
			}

			port.AddTarget(targetURL)
		}

		ports[k] = port
	}

	return ports
}

func (c *container) getLegacyPort() (model.PortConfig, error) {
	cPort := c.container.Config.Labels[LabelContainerPort]
	if cPort == "" {
		cPort = c.getIntenalPort()
	}

	cProtocol, hasProtocol := c.container.Config.Labels[LabelScheme]
	if !hasProtocol {
		cProtocol = "http"
	}

	port, err := model.NewPortLongLabel("443/https:" + cPort + "/" + cProtocol)
	if err != nil {
		return port, err
	}

	port.TLSValidate = c.getLabelBool(LabelTLSValidate, model.DefaultTLSValidate)
	port.Tailscale.Funnel = c.getLabelBool(LabelFunnel, model.DefaultTailscaleFunnel)

	return port, nil
}

// getTailscaleConfig method returns the tailscale configuration.
func (c *container) getTailscaleConfig() (*model.Tailscale, error) {
	authKey := c.getLabelString(LabelAuthKey, "")

	authKey, err := c.getAuthKeyFromAuthFile(authKey)
	if err != nil {
		return nil, fmt.Errorf("error setting auth key from file : %w", err)
	}

	return &model.Tailscale{
		Ephemeral:    c.getLabelBool(LabelEphemeral, model.DefaultTailscaleEphemeral),
		RunWebClient: c.getLabelBool(LabelRunWebClient, model.DefaultTailscaleRunWebClient),
		Verbose:      c.getLabelBool(LabelTsnetVerbose, model.DefaultTailscaleVerbose),
		AuthKey:      authKey,
	}, nil
}

// getLabelBool method returns a bool from a container label.
func (c *container) getLabelBool(label string, defaultValue bool) bool {
	// Set default value
	value := defaultValue
	if valueString, ok := c.container.Config.Labels[label]; ok {
		valueBool, err := strconv.ParseBool(valueString)
		// set value only if no error
		// if error, keep default
		//
		if err == nil {
			value = valueBool
		}
	}
	return value
}

// getLabelString method returns a string from a container label.
func (c *container) getLabelString(label string, defaultValue string) string {
	// Set default value
	value := defaultValue
	if valueString, ok := c.container.Config.Labels[label]; ok {
		value = valueString
	}

	return value
}

// getAuthKeyFromAuthFile method returns a auth key from a file.
func (c *container) getAuthKeyFromAuthFile(authKey string) (string, error) {
	authKeyFile, ok := c.container.Config.Labels[LabelAuthKeyFile]
	if !ok || authKeyFile == "" {
		return authKey, nil
	}
	temp, err := os.ReadFile(authKeyFile)
	if err != nil {
		return "", fmt.Errorf("read auth key from file: %w", err)
	}
	return strings.TrimSpace(string(temp)), nil
}

// getIntenalPort method returns the container internal port
func (c *container) getIntenalPort() string {
	// If Label is defined, get the container port
	if customContainerPort, ok := c.container.Config.Labels[LabelContainerPort]; ok {
		return customContainerPort
	}

	for p := range c.container.NetworkSettings.Ports {
		return p.Port()
	}
	// in network_mode=host
	for p := range c.container.HostConfig.PortBindings {
		return p.Port()
	}

	return ""
}

// getExposedPort method returns the container port
func (c *container) getExposedPort(internalPort string) string {
	for p, b := range c.container.HostConfig.PortBindings {
		if p.Port() == internalPort {
			return b[0].HostPort
		}
	}

	// return the first exposed port
	for _, bindings := range c.container.HostConfig.PortBindings {
		if len(bindings) > 0 {
			return bindings[0].HostPort
		}
	}

	return ""
}

func (c *container) getImagePort() string {
	for p := range c.image.Config.ExposedPorts {
		return p.Port()
	}
	return ""
}

// getProxyURL method returns the proxy URL from the container label.
func (c *container) getProxyURL() (*url.URL, error) {
	// set default proxy URL
	name := c.getName()

	// Set custom proxy URL if present the Label in the container
	if customName, ok := c.container.Config.Labels[LabelName]; ok {
		name = customName
	}

	// validate url
	return url.Parse("https://" + name)
}

// getName method returns the name of the container
func (c *container) getName() string {
	return strings.TrimLeft(c.container.Name, "/")
}

// getTargetURL method returns the container target URL
func (c *container) getTargetURL(hostname, internalPort string) (*url.URL, error) {
	if internalPort == "" {
		internalPort = c.getIntenalPort()
	}
	exposedPort := c.getExposedPort(internalPort)
	imagePort := c.getImagePort()

	if exposedPort == "" && internalPort == "" && imagePort == "" {
		return nil, ErrNoPortFoundInContainer
	}

	// return localhost if container same as host to serve the dashboard
	if osname, err := os.Hostname(); err == nil && strings.HasPrefix(c.container.ID, osname) {
		return url.Parse("http://127.0.0.1:" + internalPort)
	}

	// set autodetect
	if c.autodetect {
		// repeat auto detect in case the container is not ready
		for try := range autoDetectTries {
			c.log.Info().Int("try", try).Msg("Trying to auto detect target URL")
			if port, err := c.tryConnectContainer(hostname, internalPort, exposedPort, imagePort); err == nil {
				return port, nil
			}
			// wait to container get ready
			time.Sleep(autoDetectSleep)
		}
	}

	// auto detect failed or was disabled
	port := exposedPort
	if port == "" {
		port = internalPort
	}

	return url.Parse(c.scheme + "://" + c.defaultTargetHostname + ":" + port)
}

// tryConnectContainer method tries to connect to the container
func (c *container) tryConnectContainer(hostname, internalPort, exposedPort, imagePort string) (*url.URL, error) {
	// test connection with the container using docker networking
	// try connecting to internal ip and internal port
	if internalPort != "" {
		port, err := c.tryInternalPort(hostname, internalPort)
		if err == nil {
			return port, nil
		}
		c.log.Debug().Err(err).Msg("Error connecting to internal port")
	}

	// try connecting to internal gateway and exposed port
	if exposedPort != "" {
		port, err := c.tryExposedPort(hostname, exposedPort)
		if err == nil {
			return port, nil
		}
		c.log.Debug().Err(err).Msg("Error connecting to exposed port")
	}

	if imagePort != "" {
		port, err := c.tryInternalPort(hostname, imagePort)
		if err == nil {
			return port, nil
		}
		port, err = c.tryExposedPort(hostname, imagePort)
		if err == nil {
			return port, nil
		}

		c.log.Debug().Err(err).Msg("Error to connect using image port")
	}

	return nil, &NoValidTargetFoundError{containerName: c.container.Name}
}

// tryInternalPort method tries to connect to the container internal ip and internal port
func (c *container) tryInternalPort(hostname, port string) (*url.URL, error) {
	c.log.Debug().Str("hostname", hostname).Str("port", port).Msg("trying to connect to internal port")
	for _, network := range c.container.NetworkSettings.Networks {
		if network.IPAddress == "" {
			continue
		}
		// try connecting to container IP and internal port
		if err := c.dial(network.IPAddress, port); err == nil {
			c.log.Info().Str("address", network.IPAddress).
				Str("port", port).Msg("Successfully connected using internal ip and internal port")
			return url.Parse(c.scheme + "://" + network.IPAddress + ":" + port)
		}
		c.log.Debug().Str("address", network.IPAddress).
			Str("port", port).Msg("Failed to connect")
	}
	// if the container is running in host mode,
	// try connecting to defaultBridgeAddress of the host and internal port.
	if c.container.HostConfig.NetworkMode == "host" && c.defaultBridgeAddress != "" {
		if err := c.dial(c.defaultBridgeAddress, port); err == nil {
			c.log.Info().Str("address", c.defaultBridgeAddress).Str("port", port).Msg("Successfully connected using defaultBridgeAddress and internal port")
			return url.Parse(c.scheme + "://" + c.defaultBridgeAddress + ":" + port)
		}

		c.log.Debug().Str("address", c.defaultBridgeAddress).Str("port", port).Msg("Failed to connect")
	}

	return nil, ErrNoValidTargetFoundForInternalPorts
}

// tryExposedPort method tries to connect to the container internal ip and exposed port
func (c *container) tryExposedPort(hostname, port string) (*url.URL, error) {
	for _, network := range c.container.NetworkSettings.Networks {
		if err := c.dial(network.Gateway, port); err == nil {
			c.log.Info().Str("address", network.Gateway).Str("port", port).Msg("Successfully connected using docker network gateway and exposed port")
			return url.Parse(c.scheme + "://" + network.Gateway + ":" + port)
		}

		c.log.Debug().Str("address", network.Gateway).Str("port", port).Msg("Failed to connect using docker network gateway and exposed port")
	}

	// try connecting to configured host and exposed port
	if err := c.dial(hostname, port); err == nil {
		c.log.Info().Str("address", hostname).Str("port", port).Msg("Successfully connected using configured host and exposed port")
		return url.Parse(c.scheme + "://" + hostname + ":" + port)
	}

	c.log.Debug().Str("address", hostname).Str("port", port).Msg("Failed to connect")
	return nil, ErrNoValidTargetFoundForExposedPorts
}

// dial method tries to connect to a host and port
func (c *container) dial(host, port string) error {
	address := host + ":" + port
	conn, err := net.DialTimeout("tcp", address, dialTimeout)
	if err != nil {
		return fmt.Errorf("error dialing %s: %w", address, err)
	}
	conn.Close()

	return nil
}
