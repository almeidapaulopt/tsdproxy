// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package docker

import (
	"errors"
)

type NoValidTargetFoundError struct {
	containerName string
}

func (n *NoValidTargetFoundError) Error() string {
	return "no valid target found for " + n.containerName
}

var (
	ErrNoPortFoundInContainer              = errors.New("no port found in container")
	ErrNoValidTargetFoundForInternalPorts  = errors.New("no valid target found for internal ports")
	ErrNoValidTargetFoundForPublishedPorts = errors.New("no valid target found for exposed ports")
)
