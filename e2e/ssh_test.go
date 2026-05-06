// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/ssh"
)

func TestE2ESSHForward(t *testing.T) {
	authKey := requireTailscaleAuth(t)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	ctr := StartContainer(t, ContainerConfig{
		Image:        "linuxserver/openssh-server:latest",
		ExposedPorts: []string{"2222/tcp"},
		Env: map[string]string{
			"PUID":            "1000",
			"PGID":            "1000",
			"PASSWORD_ACCESS": "true",
			"USER_PASSWORD":   "testpass",
			"USER_NAME":       "testuser",
		},
		WaitPort: "2222/tcp",
	})

	host, err := ctr.Host(ctx)
	require.NoError(t, err, "failed to get container host")
	mappedPort, err := ctr.MappedPort(ctx, "2222")
	require.NoError(t, err, "failed to get mapped port for SSH container")
	// Use the testcontainers-reported host instead of hardcoding 127.0.0.1.
	// If tsdproxy runs inside a Docker container, 127.0.0.1 would point to
	// the proxy itself rather than the host. testcontainers.Host() returns
	// the correct address for the current environment.
	targetURL := fmt.Sprintf("tcp://%s:%s", host, mappedPort.Port())
	t.Logf("SSH container target: %s", targetURL)

	hostname := fmt.Sprintf("e2e-ssh-%d", time.Now().UnixNano())
	listPath := filepath.Join(e2eTestDataDir(t), "ssh-list.yaml")
	WriteListProviderFile(t, listPath, map[string]ListEntry{
		hostname: {
			ProxyProvider: "default",
			Tailscale:     ListTailscale{Ephemeral: true},
			Dashboard:     ListDashboard{Visible: true, Label: "SSH E2E"},
			Ports: map[string]ListPort{
				"443/tcp": {Targets: []string{targetURL}},
			},
		},
	})

	httpPort := getFreePort()
	tmpDir := e2eTestDataDir(t)
	dataDir := filepath.Join(tmpDir, "data")
	require.NoError(t, os.MkdirAll(dataDir, 0o755))

	configContent := fmt.Sprintf(`defaultProxyProvider: default
docker:
  local:
    host: "unix:///var/run/docker.sock"
    targetHostname: "172.17.0.1"
lists:
  testlist:
    filename: %q
tailscale:
  providers:
    default:
      authKey: %q
  dataDir: %q
http:
  hostname: "0.0.0.0"
  port: %d
log:
  level: debug
  json: false
proxyAccessLog: true
`, listPath, authKey, dataDir, httpPort)

	_ = startTSDProxyRawConfig(t, configContent, httpPort, tmpDir, dataDir)
	client := NewTSNetClient(t, authKey)

	peer := waitForPeerByDNSName(t, ctx, client, hostname, 90*time.Second)
	dnsName := strings.TrimSuffix(peer.DNSName, ".")
	t.Logf("SSH proxy running at %s", dnsName)

	dialAddr := client.ProxyTCPAddress(hostname, 443)
	conn, err := client.Dial(ctx, "tcp", dialAddr)
	require.NoError(t, err, "failed to dial TCP proxy %s", dialAddr)
	defer conn.Close()

	sshConfig := &ssh.ClientConfig{
		User: "testuser",
		Auth: []ssh.AuthMethod{ssh.Password("testpass")},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout: 30 * time.Second,
	}

	sshConn, chans, reqs, err := ssh.NewClientConn(conn, dialAddr, sshConfig)
	require.NoError(t, err, "failed to establish SSH connection")
	defer sshConn.Close()

	sshClient := ssh.NewClient(sshConn, chans, reqs)
	defer sshClient.Close()

	session, err := sshClient.NewSession()
	require.NoError(t, err, "failed to create SSH session")
	defer session.Close()

	cmd := "echo hello-ssh-e2e"
	output, err := session.CombinedOutput(cmd)
	require.NoError(t, err, "failed to execute command over SSH")
	require.Contains(t, strings.TrimSpace(string(output)), "hello-ssh-e2e",
		"unexpected SSH command output")

	t.Logf("SSH command output verified: %q", strings.TrimSpace(string(output)))
}
