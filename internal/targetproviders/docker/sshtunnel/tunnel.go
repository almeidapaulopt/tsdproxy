// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package sshtunnel

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog"
	"golang.org/x/crypto/ssh"
)

const (
	defaultSSHPort    = 22
	dockerDialStdio   = "docker system dial-stdio"
	sshConnectTimeout = 30 * time.Second
)

type (
	Tunnel struct {
		log       zerolog.Logger
		sshCfg    *ssh.ClientConfig
		conn      *ssh.Client
		agentConn net.Conn
		host      string
		user      string
		cfg       Config
		mu        sync.Mutex
		closed    bool
	}
)

func New(cfg Config, log zerolog.Logger) (*Tunnel, error) {
	if !strings.HasPrefix(cfg.Host, "ssh://") {
		return nil, ErrNotSSHHost
	}

	parsed, err := url.Parse(cfg.Host)
	if err != nil {
		return nil, fmt.Errorf("error parsing SSH URL %q: %w", cfg.Host, err)
	}

	host := parsed.Hostname()
	port := parsed.Port()
	if port == "" {
		port = strconv.Itoa(defaultSSHPort)
	}

	user := parsed.User.Username()
	if user == "" {
		user = "root"
	}

	authMethods, agentConn, err := authMethods(cfg, log)
	if err != nil {
		return nil, fmt.Errorf("error configuring SSH auth: %w", err)
	}

	hostKeyCB, err := hostKeyCallback(cfg)
	if err != nil {
		return nil, fmt.Errorf("error configuring SSH host key verification: %w", err)
	}

	sshCfg := &ssh.ClientConfig{
		User:            user,
		Auth:            authMethods,
		HostKeyCallback: hostKeyCB,
		Timeout:         sshConnectTimeout,
	}

	return &Tunnel{
		log:       log.With().Str("component", "sshtunnel").Str("host", host).Logger(),
		cfg:       cfg,
		sshCfg:    sshCfg,
		agentConn: agentConn,
		host:      net.JoinHostPort(host, port),
		user:      user,
	}, nil
}

func (t *Tunnel) DialContext(ctx context.Context, _, _ string) (net.Conn, error) {
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return nil, ErrTunnelClosed
	}

	if err := t.ensureConnected(ctx); err != nil {
		t.mu.Unlock()
		return nil, fmt.Errorf("ssh tunnel connect: %w", err)
	}
	conn := t.conn
	t.mu.Unlock()

	session, err := conn.NewSession()
	if err != nil {
		t.invalidateConnection(conn)
		return nil, fmt.Errorf("ssh session: %w", err)
	}

	sconn, err := newSessionConn(session)
	if err != nil {
		session.Close()
		t.invalidateConnection(conn)
		return nil, fmt.Errorf("ssh session pipes: %w", err)
	}

	if err := session.Start(dockerDialStdio); err != nil {
		sconn.Close()
		t.invalidateConnection(conn)
		return nil, fmt.Errorf("ssh exec %q: %w", dockerDialStdio, err)
	}

	t.log.Trace().Msg("ssh session established for docker system dial-stdio")
	return sconn, nil
}

func (t *Tunnel) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.closed = true
	if t.agentConn != nil {
		_ = t.agentConn.Close()
		t.agentConn = nil
	}
	if t.conn != nil {
		err := t.conn.Close()
		t.conn = nil
		return err
	}
	return nil
}

func (t *Tunnel) ensureConnected(ctx context.Context) error {
	if t.conn != nil {
		return nil
	}

	t.log.Debug().Str("user", t.user).Msg("establishing SSH connection")

	d := net.Dialer{Timeout: t.sshCfg.Timeout}
	tcpConn, err := d.DialContext(ctx, "tcp", t.host)
	if err != nil {
		return fmt.Errorf("ssh tcp dial %s: %w", t.host, err)
	}

	type handshakeResult struct {
		conn  *ssh.Client
		err   error
		close func()
	}

	ch := make(chan handshakeResult, 1)
	go func() {
		sshConn, chans, reqs, err := ssh.NewClientConn(tcpConn, t.host, t.sshCfg)
		if err != nil {
			ch <- handshakeResult{err: err, close: func() { tcpConn.Close() }}
			return
		}
		ch <- handshakeResult{
			conn:  ssh.NewClient(sshConn, chans, reqs),
			close: func() {},
		}
	}()

	select {
	case <-ctx.Done():
		go func() {
			if r := <-ch; r.close != nil {
				r.close()
			}
		}()
		return fmt.Errorf("ssh handshake canceled: %w", ctx.Err())
	case r := <-ch:
		if r.err != nil {
			r.close()
			return fmt.Errorf("ssh handshake: %w", r.err)
		}
		t.conn = r.conn
	}

	t.log.Info().Msg("SSH connection established")

	return nil
}

// invalidateConnection closes and clears the shared SSH client, but only if it
// is still the same connection that failed. This prevents a concurrent dial
// from closing a connection that another goroutine has already replaced.
func (t *Tunnel) invalidateConnection(stale *ssh.Client) {
	t.mu.Lock()
	if t.conn != nil && t.conn == stale {
		_ = t.conn.Close()
		t.conn = nil
	}
	t.mu.Unlock()
}
