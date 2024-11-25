// SPDX-FileCopyrightText: 2024 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT
package docker

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/rs/zerolog"

	"github.com/almeidapaulopt/tsdproxy/internal/proxyconfig"
)

const (
	// Constants to be used in container labels
	LabelPrefix    = "tsdproxy."
	LabelIsEnabled = LabelEnable + "=true"

	// Container config labels.
	LabelEnable             = LabelPrefix + "enable"
	LabelName               = LabelPrefix + "name"
	LabelContainerPort      = LabelPrefix + "container_port"
	LabelEphemeral          = LabelPrefix + "ephemeral"
	LabelRunWebClient       = LabelPrefix + "runwebclient"
	LabelTsnetVerbose       = LabelPrefix + "tsnet_verbose"
	LabelFunnel             = LabelPrefix + "funnel"
	LabelAuthKey            = LabelPrefix + "authkey"
	LabelAuthKeyFile        = LabelPrefix + "authkeyfile"
	LabelContainerAccessLog = LabelPrefix + "containeraccesslog"
	LabelProxyProvider      = LabelPrefix + "proxyprovider"

	//
	dialTimeout = 3 * time.Second
)

// container struct stores the data from the docker container.
type container struct {
	log                   zerolog.Logger
	container             types.ContainerJSON
	defaultTargetHostname string
	targetprovider        string
}

// newContainer function returns a new container.
func newContainer(logger zerolog.Logger, dcontainer types.ContainerJSON, targetprovider string, defaultTargetHostname string) *container {
	return &container{
		log:                   logger.With().Str("container", dcontainer.Name).Logger(),
		container:             dcontainer,
		defaultTargetHostname: defaultTargetHostname,
		targetprovider:        targetprovider,
	}
}

// newProxyConfig method returns a new proxyconfig.Config.
func (c *container) newProxyConfig() (*proxyconfig.Config, error) {
	// Get the proxy URL
	//
	proxyURL, err := c.getProxyURL()
	if err != nil {
		return nil, fmt.Errorf("error parsing Hostname: %w", err)
	}

	// Get the proxy URL
	targetURL, err := c.getTargetURL(c.defaultTargetHostname)
	if err != nil {
		return nil, fmt.Errorf("error parsing target hostname: %w", err)
	}

	// Get the Tailscale configuration
	tailscale, err := c.getTailscaleConfig()
	if err != nil {
		return nil, err
	}

	return &proxyconfig.Config{
		TargetID:       c.container.ID,
		TargetURL:      targetURL,
		ProxyURL:       proxyURL,
		Hostname:       proxyURL.Hostname(),
		TargetProvider: c.targetprovider,
		Tailscale:      tailscale,
		ProxyProvider:  c.getLabelString(LabelProxyProvider, proxyconfig.ProxyProvider),
		ProxyAccessLog: c.getLabelBool(LabelContainerAccessLog, proxyconfig.ProxyAccessLog),
	}, nil
}

// getTailscaleConfig method returns the tailscale configuration.
func (c *container) getTailscaleConfig() (*proxyconfig.Tailscale, error) {
	authKey := c.getLabelString(LabelAuthKey, "")

	authKey, err := c.getAuthKeyFromAuthFile(authKey)
	if err != nil {
		return nil, fmt.Errorf("error setting auth key from file : %w", err)
	}

	return &proxyconfig.Tailscale{
		Ephemeral:    c.getLabelBool(LabelEphemeral, proxyconfig.TailscaleEphemeral),
		RunWebClient: c.getLabelBool(LabelRunWebClient, proxyconfig.TailscaleRunWebClient),
		TsnetVerbose: c.getLabelBool(LabelTsnetVerbose, proxyconfig.TailscaleVerbose),
		Funnel:       c.getLabelBool(LabelFunnel, proxyconfig.TailscaleFunnel),
		AuthKey:      authKey,
		// TODO: add controlURL
		// ControlURL:         c.,
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

func (c *container) getIntenalPort() (string, bool) {
	// If Label is defined, get the container port
	if customContainerPort, ok := c.container.Config.Labels[LabelContainerPort]; ok {
		return customContainerPort, true
	}

	for p := range c.container.HostConfig.PortBindings {
		return p.Port(), true
	}

	return "", false
}

// getExposedPort method returns the container port
func (c *container) getExposedPort() (string, bool) {
	// If Label is defined, get the container port
	if customContainerPort, ok := c.container.Config.Labels[LabelContainerPort]; ok {
		return customContainerPort, true
	}

	for _, bindings := range c.container.HostConfig.PortBindings {
		if len(bindings) > 0 {
			return bindings[0].HostPort, true
		}
	}

	return "", false
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
func (c *container) getTargetURL(hostname string) (*url.URL, error) {
	tempHost := hostname
	// return localhost if container same as host to serve the dashboard
	if osname, err := os.Hostname(); err == nil && strings.HasPrefix(c.container.ID, osname) {
		tempHost = "127.0.0.1"
	}

	// Set default proxy URL (virtual server in Tailscale)
	exposedPort, ok := c.getExposedPort()
	internalPort, ok1 := c.getIntenalPort()
	if !ok && !ok1 {
		return nil, errors.New("no port found in container")
	}

	// test connection with the container using docker networking
	// try connecting to internal ip's and internal port
	for _, y := range c.container.NetworkSettings.Networks {
		if err := c.dial(y.IPAddress, internalPort); err == nil {
			c.log.Info().Msgf("Successfully connected to %s:%s", y.IPAddress, internalPort)
			return url.Parse(fmt.Sprintf("http://%s:%s", y.IPAddress, internalPort))
		}
	}

	// try connecting to internal gateway and exposed port
	for _, y := range c.container.NetworkSettings.Networks {
		if err := c.dial(y.Gateway, exposedPort); err == nil {
			c.log.Info().Msgf("Successfully connected to %s:%s", y.Gateway, exposedPort)
			return url.Parse(fmt.Sprintf("http://%s:%s", y.Gateway, exposedPort))
		}
	}

	// try connecting to configured host and exposed port
	if err := c.dial(tempHost, exposedPort); err != nil {
		c.log.Info().Msgf("Successfully connected to %s:%s", tempHost, exposedPort)
		return url.Parse(fmt.Sprintf("http://%s:%s", tempHost, exposedPort))
	}

	return nil, errors.New("no valid target found")
}

func (c *container) dial(host, port string) error {
	target := fmt.Sprintf("%s:%s", host, port)
	conn, err := net.DialTimeout("tcp", target, dialTimeout)
	if err != nil {
		return fmt.Errorf("error dialing %s: %w", target, err)
	}
	conn.Close()
	return nil
}
