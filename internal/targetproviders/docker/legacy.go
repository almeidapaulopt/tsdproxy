// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package docker

import (
	"sort"

	"github.com/almeidapaulopt/tsdproxy/internal/model"
)

func (c *container) getLegacyPort() (model.PortConfig, error) {
	c.log.Trace().Msg("getLegacyPort")
	defer c.log.Trace().Msg("end getLegacyPort")

	cPort := c.getInternalPortLegacy()

	cProtocol, hasProtocol := c.labels[LabelScheme]
	if !hasProtocol {
		cProtocol = DefaultTargetScheme
	}

	port, err := model.NewPortLongLabel("443/https:" + cPort + "/" + cProtocol)
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

func (c *container) getInternalPortLegacy() string {
	c.log.Trace().Msg("getInternalPortLegacy")
	defer c.log.Trace().Msg("end getInternalPortLegacy")

	if customContainerPort, ok := c.labels[LabelContainerPort]; ok {
		return customContainerPort
	}

	keys := make([]string, 0, len(c.ports))
	for k := range c.ports {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if len(keys) > 0 {
		return keys[0]
	}

	return ""
}
