// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package sshtunnel

import (
	"fmt"
	"net"
	"os"

	"github.com/rs/zerolog"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

// authMethods builds SSH auth methods from config. Tries private key file
// first, then SSH agent if configured. Returns ErrNoAuthMethods if neither
// produces a valid auth method.
func authMethods(cfg Config, log zerolog.Logger) ([]ssh.AuthMethod, net.Conn, error) {
	var methods []ssh.AuthMethod

	if m, err := privateKeyAuth(cfg, log); err != nil {
		return nil, nil, fmt.Errorf("ssh private key auth: %w", err)
	} else if m != nil {
		methods = append(methods, m)
	}

	agentMethod, agentConn, err := agentAuth(cfg, log)
	if err != nil {
		log.Warn().Err(err).Msg("SSH agent auth failed, skipping")
	} else if agentMethod != nil {
		methods = append(methods, agentMethod)
	}

	if len(methods) == 0 {
		return nil, nil, ErrNoAuthMethods
	}

	return methods, agentConn, nil
}

func privateKeyAuth(cfg Config, log zerolog.Logger) (ssh.AuthMethod, error) {
	if cfg.PrivateKeyFile == "" {
		return nil, nil //nolint:nilnil // no key configured is not an error; caller skips nil methods
	}

	keyBytes, err := os.ReadFile(cfg.PrivateKeyFile)
	if err != nil {
		return nil, fmt.Errorf("error reading private key file %q: %w", cfg.PrivateKeyFile, err)
	}

	var signer ssh.Signer
	if cfg.PrivateKeyPassphrase != "" {
		signer, err = ssh.ParsePrivateKeyWithPassphrase(keyBytes, []byte(cfg.PrivateKeyPassphrase))
	} else {
		signer, err = ssh.ParsePrivateKey(keyBytes)
	}
	if err != nil {
		return nil, fmt.Errorf("error parsing private key: %w", err)
	}

	log.Info().Str("keyFile", cfg.PrivateKeyFile).Msg("SSH private key loaded")
	return ssh.PublicKeys(signer), nil
}

func agentAuth(cfg Config, log zerolog.Logger) (ssh.AuthMethod, net.Conn, error) {
	socketPath := cfg.AgentSocket
	if socketPath == "" {
		socketPath = os.Getenv("SSH_AUTH_SOCK")
	}
	if socketPath == "" {
		return nil, nil, nil //nolint:nilnil // no agent configured is not an error; caller skips nil methods
	}

	conn, err := net.Dial("unix", socketPath) //nolint:gosec // socketPath is operator-supplied config, not untrusted input
	if err != nil {
		return nil, nil, fmt.Errorf("error connecting to SSH agent at %q: %w", socketPath, err)
	}

	agentClient := agent.NewClient(conn)
	signers, err := agentClient.Signers()
	if err != nil {
		conn.Close()
		return nil, nil, fmt.Errorf("error getting signers from SSH agent: %w", err)
	}

	if len(signers) == 0 {
		conn.Close()
		return nil, nil, fmt.Errorf("SSH agent at %q has no identities", socketPath)
	}

	log.Info().Str("socket", socketPath).Int("identities", len(signers)).Msg("SSH agent connected")
	return ssh.PublicKeysCallback(agentClient.Signers), conn, nil
}
