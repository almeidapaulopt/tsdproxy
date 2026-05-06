// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package docker

import "github.com/almeidapaulopt/tsdproxy/internal/model"

func (c *container) getLegacyPort() (model.PortConfig, error) {
	c.log.Trace().Msg("getLegacyPort")
	defer c.log.Trace().Msg("end getLegacyPort")

	cPort := c.getIntenalPortLegacy()

	cProtocol, hasProtocol := c.labels[LabelScheme]
	if !hasProtocol {
		cProtocol = "http"
	}

	// The legacy proxy side always uses HTTP (port 80) unless Funnel is enabled.
	// Funnel requires HTTPS. Using HTTP on the proxy side avoids ACME TLS cert
	// provisioning that can fail under rate limits when many proxies start at once.
	// The target protocol (cProtocol) is independent and comes from the scheme label.
	proxySide := "80/http"
	if c.getLabelBool(LabelFunnel, model.DefaultTailscaleFunnel) {
		proxySide = "443/https"
	}

	port, err := model.NewPortLongLabel(proxySide + ":" + cPort + "/" + cProtocol)
	if err != nil {
		return port, err
	}
	port.TLSValidate = c.getLabelBool(LabelTLSValidate, model.DefaultTLSValidate)
	port.Tailscale.Funnel = c.getLabelBool(LabelFunnel, model.DefaultTailscaleFunnel)

	port, err = c.generateTargetFromFirstTarget(port)
	if err != nil {
		return port, err
	}

	return port, nil
}

// getIntenalPortLegacy method returns the container internal port
func (c *container) getIntenalPortLegacy() string {
	c.log.Trace().Msg("getIntenalPortLegacy")
	defer c.log.Trace().Msg("end getIntenalPortLegacy")

	// If Label is defined, get the container port
	if customContainerPort, ok := c.labels[LabelContainerPort]; ok {
		return customContainerPort
	}

	for p := range c.ports {
		return p
	}

	return ""
}
