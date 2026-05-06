// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package docker

import (
	"fmt"
	"net"
	"net/url"
)

// tryConnectContainer method tries to connect to the container
func (c *container) tryConnectContainer(scheme, internalPort, publishedPort string) (*url.URL, error) {
	hostname := c.hostname

	if internalPort != "" {
		// test connection with the container using docker networking
		// try connecting to internal ip and internal port
		port, err := c.tryInternalPort(scheme, hostname, internalPort)
		if err == nil {
			return port, nil
		}
		c.log.Debug().Err(err).Msg("Error connecting to internal port")
	}

	// try connecting to internal gateway and published port
	if publishedPort != "" {
		port, err := c.tryPublishedPort(scheme, publishedPort)
		if err == nil {
			return port, nil
		}
		c.log.Debug().Err(err).Msg("Error connecting to published port")
	}

	return nil, &NoValidTargetFoundError{containerName: c.name}
}

// tryInternalPort method tries to connect to the container internal ip and internal port
func (c *container) tryInternalPort(scheme, hostname, port string) (*url.URL, error) {
	c.log.Debug().Str("hostname", hostname).Str("port", port).Msg("trying to connect to internal port")

	// if the container is running in host mode,
	// try connecting to defaultBridgeAddress of the host and internal port.
	if c.networkMode.IsHost() && c.defaultBridgeAddress.IsValid() {
		addr := c.defaultBridgeAddress.String()
		if err := c.dial(addr, port); err == nil {
			c.log.Info().Str("address", addr).Str("port", port).Msg("Successfully connected using defaultBridgeAddress and internal port")
			return url.Parse(scheme + "://" + addr + ":" + port)
		}

		c.log.Debug().Str("address", addr).Str("port", port).Msg("Failed to connect")
	}

	for _, ipAddress := range c.ipAddress {
		ip := ipAddress.String()
		// try connecting to container IP and internal port
		if err := c.dial(ip, port); err == nil {
			c.log.Info().Str("address", ip).
				Str("port", port).Msg("Successfully connected using internal ip and internal port")
			return url.Parse(scheme + "://" + ip + ":" + port)
		}
		c.log.Debug().Str("address", ip).
			Str("port", port).Msg("Failed to connect")
	}

	return nil, ErrNoValidTargetFoundForInternalPorts
}

// tryPublishedPort method tries to connect to the container internal ip and published port
func (c *container) tryPublishedPort(scheme, port string) (*url.URL, error) {
	for _, gateway := range c.gateways {
		gw := gateway.String()
		if err := c.dial(gw, port); err == nil {
			c.log.Info().Str("address", gw).Str("port", port).Msg("Successfully connected using docker network gateway and published port")
			return url.Parse(scheme + "://" + gw + ":" + port)
		}

		c.log.Debug().Str("address", gw).Str("port", port).Msg("Failed to connect using docker network gateway and published port")
	}

	return nil, ErrNoValidTargetFoundForPublishedPorts
}

// dial method tries to connect to a host and port
func (c *container) dial(host, port string) error {
	address := net.JoinHostPort(host, port)
	conn, err := net.DialTimeout("tcp", address, dialTimeout)
	if err != nil {
		return fmt.Errorf("error dialing %s: %w", address, err)
	}
	conn.Close()

	return nil
}
