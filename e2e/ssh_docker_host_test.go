// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

//go:build e2e

package e2e

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"golang.org/x/crypto/ssh"
)

// TestE2ESSHDockerHost verifies that tsdproxy can connect to a remote Docker
// daemon over an SSH tunnel (ssh:// host URI) and proxy containers discovered
// on that remote daemon.
//
// Architecture:
//   - A Docker-in-Docker (dind) container runs an isolated Docker daemon.
//   - An SSH server is set up inside the dind container with key-based auth.
//   - TSDProxy is configured with ssh:// pointing to the dind container.
//   - An nginx container is started inside the dind daemon via SSH.
//   - The test verifies TSDProxy discovers and proxies the container through
//     the SSH tunnel to the remote Docker daemon.
func TestE2ESSHDockerHost(t *testing.T) {
	authKey := requireTailscaleAuth(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// --- Generate SSH key pair ---
	_, privKeyPEM, sshSigner := generateEd25519Key(t)
	authorizedKeys := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sshSigner.PublicKey())))

	// --- Prepare test dirs ---
	tmpDir := e2eTestDataDir(t)
	sshDir := filepath.Join(tmpDir, "ssh")
	require.NoError(t, os.MkdirAll(sshDir, 0o755))

	privKeyPath := filepath.Join(sshDir, "id_ed25519")
	require.NoError(t, os.WriteFile(privKeyPath, privKeyPEM, 0o600))

	// --- Start Docker-in-Docker container with SSH server ---
	// The dind container runs its own isolated Docker daemon plus an SSH server.
	// tsdproxy connects to THIS daemon over SSH, not the host Docker daemon.
	dindCtr := StartContainer(t, ContainerConfig{
		Image:        "docker:dind",
		ExposedPorts: []string{"2222/tcp"},
		Env: map[string]string{
			"DOCKER_TLS_CERTDIR": "",
		},
		Privileged: true,
		WaitPort:   "skip",
	})

	waitForDindDockerd(ctx, t, dindCtr)

	installAndStartSSH(ctx, t, dindCtr, authorizedKeys)

	waitForSSH(ctx, t, dindCtr)

	dindHost, err := dindCtr.Host(ctx)
	require.NoError(t, err, "failed to get dind container host")
	dindPort, err := dindCtr.MappedPort(ctx, "2222")
	require.NoError(t, err, "failed to get mapped SSH port")

	knownHostsEntry := getSSHHostKey(t, ctx, dindHost, dindPort.Port())

	knownHostsPath := filepath.Join(sshDir, "known_hosts")
	require.NoError(t, os.WriteFile(knownHostsPath, []byte(knownHostsEntry+"\n"), 0o600))

	sshAddr := fmt.Sprintf("ssh://root@%s:%s", dindHost, dindPort.Port())
	t.Logf("SSH Docker host URI: %s", sshAddr)

	hostname := fmt.Sprintf("e2e-sshdocker-%d", time.Now().UnixNano())
	startContainerViaSSH(ctx, t, dindHost, dindPort.Port(), privKeyPath, hostname)

	dindIP, err := dindCtr.ContainerIP(ctx)
	require.NoError(t, err, "failed to get dind container IP")

	proxy := StartTSDProxy(t, TSDProxyConfig{
		AuthKey:             authKey,
		DockerHost:          sshAddr,
		TargetHostname:      dindIP,
		SSHPrivateKeyFile:   privKeyPath,
		SSHKnownHostsFile:   knownHostsPath,
		SSHInsecureSkipHost: false,
	})
	t.Logf("tsdproxy started on port %d with SSH Docker host", proxy.HTTPPort)

	client := NewTSNetClient(t, authKey)
	proxyURL := client.ProxyURL(hostname)

	WaitForProxyReachable(t, ctx, client, proxyURL, 150*time.Second)
	VerifyHTTPResponse(t, ctx, client, proxyURL, "Welcome to nginx!")

	t.Logf("SSH Docker host proxy verified successfully")
}

func waitForDindDockerd(ctx context.Context, t *testing.T, ctr testcontainers.Container) {
	t.Helper()

	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			t.Fatalf("context cancelled waiting for dockerd: %v", ctx.Err())
		default:
		}

		exitCode, _, err := ctr.Exec(ctx, []string{"docker", "info"})
		if err == nil && exitCode == 0 {
			t.Logf("dind dockerd is ready")
			return
		}
		time.Sleep(3 * time.Second)
	}
	t.Fatal("timed out waiting for dind dockerd")
}

func installAndStartSSH(ctx context.Context, t *testing.T, ctr testcontainers.Container, authorizedKeys string) {
	t.Helper()

	exitCode, output, err := ctr.Exec(ctx, []string{
		"sh", "-c",
		fmt.Sprintf(`set -e
apk add --no-cache openssh-server openssh-keygen
mkdir -p /root/.ssh
echo '%s' > /root/.ssh/authorized_keys
chmod 600 /root/.ssh/authorized_keys
ssh-keygen -A
cat > /etc/ssh/sshd_config <<'SSHEOF'
Port 2222
PermitRootLogin yes
PubkeyAuthentication yes
AuthorizedKeysFile .ssh/authorized_keys
SSHEOF
/usr/sbin/sshd
`, authorizedKeys),
	})
	require.NoError(t, err, "failed to exec ssh setup in dind")
	require.Equal(t, 0, exitCode, "ssh setup failed in dind: %s", readOutput(t, output))
	t.Logf("sshd installed and started in dind container")
}

func waitForSSH(ctx context.Context, t *testing.T, ctr testcontainers.Container) {
	t.Helper()

	host, err := ctr.Host(ctx)
	require.NoError(t, err)
	port, err := ctr.MappedPort(ctx, "2222")
	require.NoError(t, err)

	addr := net.JoinHostPort(host, port.Port())

	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			t.Fatalf("context cancelled waiting for SSH: %v", ctx.Err())
		default:
		}

		conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
		if err == nil {
			conn.Close()
			t.Logf("dind SSH ready at %s", addr)
			return
		}
		time.Sleep(2 * time.Second)
	}
	t.Fatal("timed out waiting for dind SSH")
}

func readOutput(t *testing.T, r io.Reader) string {
	t.Helper()
	b, err := io.ReadAll(r)
	if err != nil {
		return fmt.Sprintf("(read error: %v)", err)
	}
	return string(b)
}

// startContainerViaSSH runs an nginx container with tsdproxy labels inside the
// dind Docker daemon by executing `docker run` over SSH.
func startContainerViaSSH(ctx context.Context, t *testing.T, host, port, privKeyPath, hostname string) {
	t.Helper()

	keyBytes, err := os.ReadFile(privKeyPath)
	require.NoError(t, err, "failed to read SSH private key")

	signer, err := ssh.ParsePrivateKey(keyBytes)
	require.NoError(t, err, "failed to parse SSH private key")

	addr := net.JoinHostPort(host, port)
	d := net.Dialer{Timeout: 15 * time.Second}
	conn, err := d.DialContext(ctx, "tcp", addr)
	require.NoError(t, err, "failed to dial dind SSH")

	sshConn, chans, reqs, err := ssh.NewClientConn(conn, addr, &ssh.ClientConfig{
		User:            "root",
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(signer)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         15 * time.Second,
	})
	require.NoError(t, err, "failed to establish SSH connection to dind")

	sshClient := ssh.NewClient(sshConn, chans, reqs)
	defer sshClient.Close()

	// Run nginx with tsdproxy labels on the dind Docker daemon.
	containerName := fmt.Sprintf("nginx-%s", hostname)
	cmd := fmt.Sprintf(
		"docker run -d -p 8080:80 --name %s --label tsdproxy.enable=true --label tsdproxy.name=%s --label tsdproxy.ephemeral=true --label 'tsdproxy.port.http=443/https:80/http' nginx:alpine",
		containerName, hostname,
	)

	session, err := sshClient.NewSession()
	require.NoError(t, err, "failed to create SSH session")

	output, err := session.CombinedOutput(cmd)
	session.Close()
	require.NoError(t, err, "failed to run nginx container via SSH: %s", strings.TrimSpace(string(output)))

	t.Logf("nginx container started inside dind via SSH: %s", strings.TrimSpace(string(output)))
}

// generateEd25519Key creates an Ed25519 key pair and returns the OpenSSH
// authorized_keys entry, PEM-encoded private key, and an ssh.Signer.
func generateEd25519Key(t *testing.T) (string, []byte, ssh.Signer) {
	t.Helper()

	_, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err, "failed to generate Ed25519 key")

	signer, err := ssh.NewSignerFromKey(priv)
	require.NoError(t, err, "failed to create SSH signer")

	pubKeyStr := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(signer.PublicKey())))

	privPEM, err := ssh.MarshalPrivateKey(priv, "")
	require.NoError(t, err, "failed to marshal private key")
	pemBytes := pem.EncodeToMemory(privPEM)

	return pubKeyStr, pemBytes, signer
}

// getSSHHostKey connects to the SSH server and retrieves its host key
// in known_hosts format.
func getSSHHostKey(t *testing.T, ctx context.Context, host, port string) string {
	t.Helper()

	addr := net.JoinHostPort(host, port)

	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			t.Fatalf("context cancelled getting SSH host key: %v", ctx.Err())
		default:
		}

		var capturedKey ssh.PublicKey
		config := &ssh.ClientConfig{
			HostKeyCallback: func(_ string, _ net.Addr, key ssh.PublicKey) error {
				capturedKey = key
				return nil
			},
			Timeout: 5 * time.Second,
		}

		d := net.Dialer{Timeout: 5 * time.Second}
		conn, err := d.DialContext(ctx, "tcp", addr)
		if err != nil {
			t.Logf("getSSHHostKey: dial failed: %v, retrying...", err)
			time.Sleep(2 * time.Second)
			continue
		}

		sshConn, _, _, err := ssh.NewClientConn(conn, addr, config)
		if capturedKey != nil {
			if sshConn != nil {
				sshConn.Close()
			}
			return fmt.Sprintf("[%s]:%s %s %s",
				host, port, capturedKey.Type(), base64.StdEncoding.EncodeToString(capturedKey.Marshal()))
		}
		if err != nil {
			t.Logf("getSSHHostKey: SSH handshake failed: %v, retrying...", err)
			conn.Close()
			time.Sleep(2 * time.Second)
			continue
		}
		sshConn.Close()
		time.Sleep(time.Second)
	}

	t.Fatal("timed out getting SSH host key")
	return ""
}
