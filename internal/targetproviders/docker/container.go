// SPDX-FileCopyrightText: 2024 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT
package docker

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/docker/docker/api/types"

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
)

// container struct stores the data from the docker container.
type container struct {
	container             types.ContainerJSON
	defaultTargetHostname string
	targetprovider        string
}

// newContainer function returns a new container.
func newContainer(dcontainer types.ContainerJSON, targetprovider string, defaultTargetHostname string) *container {
	return &container{
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

// getTargetPort method returns the container port
func (c *container) getTargetPort() (string, bool) {
	// If Label is defined, get the container port
	if customContainerPort, ok := c.container.Config.Labels[LabelContainerPort]; ok {
		return customContainerPort, true
	}

	for port := range c.container.Config.ExposedPorts {
		return strconv.Itoa(port.Int()), true
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
	temp := hostname
	// return localhost if container same as host to serve the dashboard
	osname, err := os.Hostname()
	if err == nil {
		if strings.HasPrefix(c.container.ID, osname) {
			temp = "127.0.0.1"
		}
	}

	// Set default proxy URL (virtual server in Tailscale)
	containerPort, ok := c.getTargetPort()
	if !ok {
		return nil, errors.New("no port found in container")
	}

	return url.Parse(fmt.Sprintf("http://%s:%s", temp, containerPort))
}
