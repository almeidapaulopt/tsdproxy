// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package sshtunnel

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/rs/zerolog"
)

func TestNewTunnel_RejectsNonSSHHost(t *testing.T) {
	t.Parallel()

	_, err := New(Config{Host: "tcp://localhost:2375"}, zerolog.Nop())
	if !errors.Is(err, ErrNotSSHHost) {
		t.Errorf("error = %v, want ErrNotSSHHost", err)
	}
}

func TestNewTunnel_RejectsInvalidURL(t *testing.T) {
	t.Parallel()

	_, err := New(Config{Host: "ssh://\x00invalid"}, zerolog.Nop())
	if err == nil {
		t.Fatal("expected error for invalid URL")
	}
}

func TestNewTunnel_ParsesHostAndUser(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	keyFile := generateTestKey(t, dir)
	knownHostsFile := generateTestKnownHosts(t, dir)

	tunnel, err := New(Config{
		Host:           "ssh://deploy@remote.example.com:2222",
		PrivateKeyFile: keyFile,
		KnownHostsFile: knownHostsFile,
		AgentSocket:    "/dev/null",
	}, zerolog.Nop())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if tunnel.user != "deploy" {
		t.Errorf("user = %q, want %q", tunnel.user, "deploy")
	}
	if tunnel.host != "remote.example.com:2222" {
		t.Errorf("host = %q, want %q", tunnel.host, "remote.example.com:2222")
	}

	tunnel.Close()
}

func TestNewTunnel_DefaultsPortAndUser(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	keyFile := generateTestKey(t, dir)
	knownHostsFile := generateTestKnownHosts(t, dir)

	tunnel, err := New(Config{
		Host:           "ssh://remote.example.com",
		PrivateKeyFile: keyFile,
		KnownHostsFile: knownHostsFile,
		AgentSocket:    "/dev/null",
	}, zerolog.Nop())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if tunnel.user != "root" {
		t.Errorf("user = %q, want %q", tunnel.user, "root")
	}
	if tunnel.host != "remote.example.com:22" {
		t.Errorf("host = %q, want %q", tunnel.host, "remote.example.com:22")
	}

	tunnel.Close()
}

func TestNewTunnel_NoAuthMethods(t *testing.T) {
	t.Setenv("SSH_AUTH_SOCK", "")

	_, err := New(Config{
		Host:                  "ssh://host",
		InsecureSkipHostCheck: true,
	}, zerolog.Nop())
	if !errors.Is(err, ErrNoAuthMethods) {
		t.Errorf("error = %v, want ErrNoAuthMethods", err)
	}
}

func TestNewTunnel_KnownHostsRequired(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	keyFile := generateTestKey(t, dir)

	_, err := New(Config{
		Host:           "ssh://host",
		PrivateKeyFile: keyFile,
		AgentSocket:    "/dev/null",
	}, zerolog.Nop())
	if err == nil {
		t.Fatal("expected error when known_hosts not provided and insecure skip is false")
	}
}

func TestNewTunnel_InsecureSkipHostCheck(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	keyFile := generateTestKey(t, dir)

	tunnel, err := New(Config{
		Host:                  "ssh://host",
		PrivateKeyFile:        keyFile,
		InsecureSkipHostCheck: true,
		AgentSocket:           "/dev/null",
	}, zerolog.Nop())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	tunnel.Close()
}

func TestDialContext_ReturnsErrorAfterClose(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	keyFile := generateTestKey(t, dir)

	tunnel, err := New(Config{
		Host:                  "ssh://host",
		PrivateKeyFile:        keyFile,
		InsecureSkipHostCheck: true,
		AgentSocket:           "/dev/null",
	}, zerolog.Nop())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tunnel.Close()

	_, err = tunnel.DialContext(t.Context(), "tcp", "ignored")
	if !errors.Is(err, ErrTunnelClosed) {
		t.Errorf("error = %v, want ErrTunnelClosed", err)
	}
}

func TestPrivateKeyAuth_LoadsEd25519Key(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	keyFile := generateTestKey(t, dir)

	m, err := privateKeyAuth(Config{PrivateKeyFile: keyFile}, zerolog.Nop())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m == nil {
		t.Fatal("expected auth method, got nil")
	}
}

func TestPrivateKeyAuth_SkipsWhenNoFile(t *testing.T) {
	t.Parallel()

	m, err := privateKeyAuth(Config{}, zerolog.Nop())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m != nil {
		t.Fatal("expected nil when no key file configured")
	}
}

func TestPrivateKeyAuth_FailsForBadFile(t *testing.T) {
	t.Parallel()

	_, err := privateKeyAuth(Config{PrivateKeyFile: "/nonexistent/key"}, zerolog.Nop())
	if err == nil {
		t.Fatal("expected error for nonexistent key file")
	}
}

func TestHostKeyCallback_InsecureSkip(t *testing.T) {
	t.Parallel()

	cb, err := hostKeyCallback(Config{InsecureSkipHostCheck: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cb == nil {
		t.Fatal("expected callback, got nil")
	}
}

func TestHostKeyCallback_RequiresKnownHostsFile(t *testing.T) {
	t.Parallel()

	_, err := hostKeyCallback(Config{})
	if err == nil {
		t.Fatal("expected error when no known_hosts file")
	}
}

func TestHostKeyCallback_ParsesKnownHosts(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	knownHostsFile := generateTestKnownHosts(t, dir)

	cb, err := hostKeyCallback(Config{KnownHostsFile: knownHostsFile})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cb == nil {
		t.Fatal("expected callback, got nil")
	}
}

func generateTestKey(t *testing.T, dir string) string {
	t.Helper()

	keyFile := filepath.Join(dir, "id_ed25519")
	keyData := `-----BEGIN OPENSSH PRIVATE KEY-----
b3BlbnNzaC1rZXktdjEAAAAABG5vbmUAAAAEbm9uZQAAAAAAAAABAAAAMwAAAAtzc2gtZW
QyNTUxOQAAACByxG72AeEXXjBn5ubLXRErXrTRtVWuyP+4RMR1WCBZHgAAAIg44yCFOOMg
hQAAAAtzc2gtZWQyNTUxOQAAACByxG72AeEXXjBn5ubLXRErXrTRtVWuyP+4RMR1WCBZHg
AAAEAI+9ypyoA8KZBjih2Nf8FTRwd54KRNIR07+o9oxa6pbXLEbvYB4RdeMGfm5stdESte
tNG1Va7I/7hExHVYIFkeAAAABHRlc3QB
-----END OPENSSH PRIVATE KEY-----`
	if err := os.WriteFile(keyFile, []byte(keyData), 0o600); err != nil {
		t.Fatalf("failed to write test key: %v", err)
	}
	return keyFile
}

func generateTestKnownHosts(t *testing.T, dir string) string {
	t.Helper()

	knownHostsFile := filepath.Join(dir, "known_hosts")
	content := "remote.example.com ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIHLIbvYB4RdeMGfm5stdEStetNG1Va7I/7hExHVYIFke\n"
	if err := os.WriteFile(knownHostsFile, []byte(content), 0o600); err != nil {
		t.Fatalf("failed to write known_hosts: %v", err)
	}
	return knownHostsFile
}
