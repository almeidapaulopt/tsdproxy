// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package sshtunnel

import (
	"errors"
	"fmt"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

func hostKeyCallback(cfg Config) (ssh.HostKeyCallback, error) {
	if cfg.InsecureSkipHostCheck {
		return ssh.InsecureIgnoreHostKey(), nil //nolint:gosec
	}

	if cfg.KnownHostsFile == "" {
		return nil, errors.New("sshKnownHostsFile is required when sshInsecureSkipHostCheck is false")
	}

	cb, err := knownhosts.New(cfg.KnownHostsFile)
	if err != nil {
		return nil, fmt.Errorf("error parsing known_hosts file %q: %w", cfg.KnownHostsFile, err)
	}

	return cb, nil
}
