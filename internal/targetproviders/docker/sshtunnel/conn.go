// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package sshtunnel

import (
	"io"
	"net"
	"sync/atomic"
	"time"

	"golang.org/x/crypto/ssh"
)

type (
	sshSessionConn struct {
		session *ssh.Session
		stdin   io.WriteCloser
		stdout  io.Reader
		closed  atomic.Bool
	}

	addr struct{}
)

func newSessionConn(session *ssh.Session) (*sshSessionConn, error) {
	stdin, err := session.StdinPipe()
	if err != nil {
		return nil, err
	}

	stdout, err := session.StdoutPipe()
	if err != nil {
		stdin.Close()
		return nil, err
	}

	return &sshSessionConn{
		session: session,
		stdin:   stdin,
		stdout:  stdout,
	}, nil
}

func (c *sshSessionConn) Read(b []byte) (int, error)  { return c.stdout.Read(b) }
func (c *sshSessionConn) Write(b []byte) (int, error) { return c.stdin.Write(b) }

func (c *sshSessionConn) Close() error {
	if c.closed.CompareAndSwap(false, true) {
		_ = c.stdin.Close()
		_ = c.session.Close()
	}
	return nil
}

func (c *sshSessionConn) LocalAddr() net.Addr                { return addr{} }
func (c *sshSessionConn) RemoteAddr() net.Addr               { return addr{} }
func (c *sshSessionConn) SetDeadline(_ time.Time) error      { return nil }
func (c *sshSessionConn) SetReadDeadline(_ time.Time) error  { return nil }
func (c *sshSessionConn) SetWriteDeadline(_ time.Time) error { return nil }

func (addr) Network() string { return "ssh" }
func (addr) String() string  { return "ssh" }
