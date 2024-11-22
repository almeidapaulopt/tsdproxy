// SPDX-FileCopyrightText: 2024 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT
package config

import (
	"fmt"
	"os"

	"github.com/creasty/defaults"
)

const Legacy = "_legacy_"

// loadLegacyConfig method    Generate the config from environment variables
// used in 0.x.x versions
func (c *config) loadLegacyConfig() {
	// Legacy Hostname from DOCKER_HOST from environment
	//

	if os.Getenv("DOCKER_HOST") != "" {
		println("DOCKER_HOST is deprecated, use ./config/tsdproxy.yaml file instead")
		c.loadLegacyDockerConfig()
	}

	if os.Getenv("TSDPROXY_AUTHKEYFILE") != "" || os.Getenv("TSDPROXY_AUTHKEY") != "" {
		println("TSDPROXY_AUTHKEY and TSDPROXY_AUTHKEYFILE are deprecated, use ./config/tsdproxy.yaml file instead")
		c.loadLegacyTailscaleConfig()
	}
}

// loadLegacyDockerConfig method    generate the Docker Config provider from environment variables
func (c *config) loadLegacyDockerConfig() {
	// Legacy Hostname from DOCKER_HOST from environment
	//
	docker := new(DockerTargetProviderConfig)
	// set DockerConfig defaults
	if err := defaults.Set(docker); err != nil {
		fmt.Printf("Error loading defaults: %v", err)
	}
	docker.Host = os.Getenv("DOCKER_HOST")
	c.Docker[Legacy] = docker

	if os.Getenv("TSDPROXY_HOSTNAME") != "" {
		docker.TargetHostname = os.Getenv("TSDPROXY_HOSTNAME")
	}
}

// loadLegacyTailscaleConfig method  generate the Tailscale Config provider from environment variables
func (c *config) loadLegacyTailscaleConfig() {
	ts := new(TailscaleServerConfig)
	// set TailscaleConfig defaults
	if err := defaults.Set(ts); err != nil {
		fmt.Printf("Error loading defaults: %v", err)
	}

	authKeyFile := os.Getenv("TSDPROXY_AUTHKEYFILE")
	authKey := os.Getenv("TSDPROXY_AUTHKEY")
	controlURL := os.Getenv("TSDPROXY_CONTROLURL")
	dataDir := os.Getenv("TSDPROXY_DATADIR")

	if authKeyFile != "" {
		var err error
		authKey, err = c.getAuthKeyFromFile(authKeyFile)
		if err != nil {
			fmt.Printf("Error loading auth key from file: %v", err)
		}
	}

	ts.AuthKey = authKey
	ts.AuthKeyFile = authKeyFile

	if controlURL != "" {
		ts.ControlURL = controlURL
	}
	if dataDir != "" {
		c.Tailscale.DataDir = dataDir
	}

	c.Tailscale.Providers[Legacy] = ts
}
