// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package docker

import (
	"context"
	"sort"
	"strconv"

	"github.com/almeidapaulopt/tsdproxy/internal/model"
)

func (c *container) getLegacyPort(ctx context.Context) (model.PortConfig, error) {
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

	port, err = c.generateTargetFromFirstTarget(ctx, port)
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
	sort.Slice(keys, func(i, j int) bool {
		pi, errI := strconv.Atoi(keys[i])
		pj, errJ := strconv.Atoi(keys[j])
		if errI == nil && errJ == nil {
			return pi < pj
		}
		if errI != nil && errJ != nil {
			return keys[i] < keys[j]
		}
		return errI == nil // numbers sort before non-numeric
	})
	if len(keys) > 0 {
		return keys[0]
	}

	return ""
}
