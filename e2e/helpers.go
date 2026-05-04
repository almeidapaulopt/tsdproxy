// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

//go:build e2e

package e2e

import (
	"bufio"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	ctypes "github.com/moby/moby/api/types/container"
	"tailscale.com/ipn/ipnstate"
	"tailscale.com/tsnet"
)

// --- TSDProxy Instance ---

// TSDProxyInstance manages a running tsdproxy process for testing.
type TSDProxyInstance struct {
	cmd      *exec.Cmd
	BaseURL  string
	HTTPPort int
	TmpDir   string
	DataDir  string
	Config   string

	exitOnce sync.Once
	exitErr  error
	exited   atomic.Bool
}

type TSDProxyConfig struct {
	AuthKey        string
	AuthKeyFile    string
	Tags           string
	HTTPPort       int
	DataDir        string
	DockerHost     string
	TargetHostname string
	ControlURL     string
}

func defaultTSDProxyConfig() TSDProxyConfig {
	return TSDProxyConfig{
		HTTPPort:       getFreePort(),
		TargetHostname: "172.17.0.1",
	}
}

func StartTSDProxy(t *testing.T, cfg TSDProxyConfig) *TSDProxyInstance {
	t.Helper()

	ctx := context.Background()

	if cfg.HTTPPort == 0 {
		cfg.HTTPPort = getFreePort()
	}

	tmpDir := filepath.Join(e2eBaseDir, t.Name())
	require.NoError(t, os.MkdirAll(tmpDir, 0o755))
	dataDir := filepath.Join(tmpDir, "data")
	require.NoError(t, os.MkdirAll(dataDir, 0o755))

	configPath := filepath.Join(tmpDir, "tsdproxy.yaml")
	authKey := cfg.AuthKey
	if authKey == "" {
		authKey = tsAuthKey
	}

	configContent := generateConfig(configParams{
		HTTPPort:       cfg.HTTPPort,
		AuthKey:        authKey,
		AuthKeyFile:    cfg.AuthKeyFile,
		Tags:           cfg.Tags,
		DataDir:        dataDir,
		DockerHost:     cfg.DockerHost,
		TargetHostname: cfg.TargetHostname,
		ControlURL:     cfg.ControlURL,
	})
	require.NoError(t, os.WriteFile(configPath, []byte(configContent), 0o644))

	cmd := exec.CommandContext(ctx, tsdproxyBinPath, "-config", configPath)
	cmd.Env = append(os.Environ(),
		fmt.Sprintf("TSDPROXY_HTTP_PORT=%d", cfg.HTTPPort),
	)

	logFile, err := os.Create(filepath.Join(tmpDir, "tsdproxy.log"))
	require.NoError(t, err)

	cmd.Stdout = io.MultiWriter(logFile, &testLogWriter{t: t, prefix: "[tsdproxy] "})
	cmd.Stderr = io.MultiWriter(logFile, &testLogWriter{t: t, prefix: "[tsdproxy] "})

	require.NoError(t, cmd.Start(), "failed to start tsdproxy")

	instance := &TSDProxyInstance{
		cmd:     cmd,
		BaseURL: fmt.Sprintf("http://127.0.0.1:%d", cfg.HTTPPort),
		HTTPPort: cfg.HTTPPort,
		TmpDir:   tmpDir,
		DataDir:  dataDir,
		Config:   configPath,
	}
	go instance.exitOnce.Do(func() {
		instance.exitErr = cmd.Wait()
		instance.exited.Store(true)
	})

	t.Cleanup(func() {
		instance.Stop(t)
		logFile.Close()
	})

	instance.WaitReady(t)

	return instance
}

func (tp *TSDProxyInstance) Stop(t *testing.T) {
	t.Helper()
	if tp.cmd == nil || tp.cmd.Process == nil {
		return
	}

	if tp.exited.Load() {
		tp.reportExitCode(t)
		return
	}

	tp.cmd.Process.Signal(os.Interrupt)
	deadline := time.After(15 * time.Second)
	for !tp.exited.Load() {
		select {
		case <-deadline:
			tp.cmd.Process.Kill()
			return
		default:
			time.Sleep(50 * time.Millisecond)
		}
	}
	tp.reportExitCode(t)
}

func (tp *TSDProxyInstance) reportExitCode(t *testing.T) {
	var exitErr *exec.ExitError
	if errors.As(tp.exitErr, &exitErr) && exitErr.ExitCode() != 0 {
		t.Errorf("tsdproxy exited unexpectedly (exit code %d)", exitErr.ExitCode())
	}
}

func (tp *TSDProxyInstance) WaitReady(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), proxyStartupTimeout)
	defer cancel()

	for {
		select {
		case <-ctx.Done():
			t.Fatal("timed out waiting for tsdproxy to become ready")
		default:
		}

		if tp.exited.Load() {
			var exitErr *exec.ExitError
			exitCode := -1
			if errors.As(tp.exitErr, &exitErr) {
				exitCode = exitErr.ExitCode()
			}
			t.Fatalf("tsdproxy exited during startup (exit code %d)", exitCode)
		}

		resp, err := http.Get(tp.BaseURL + "/health/ready/")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(proxyReadyPollInterval)
	}
}

func (tp *TSDProxyInstance) ReadLogFile(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(tp.TmpDir, "tsdproxy.log"))
	require.NoError(t, err, "failed to read tsdproxy log file")
	return string(data)
}

// --- Docker Test Containers ---

type ContainerConfig struct {
	Image        string
	Labels       map[string]string
	ExposedPorts []string
	NetworkMode  string
	Networks     []string
	Cmd          []string
	Env          map[string]string
	WaitPort     string // port to wait for before returning (e.g. "80/tcp", "8080/tcp"). Defaults to "80/tcp".
}

func StartContainer(t *testing.T, cfg ContainerConfig) testcontainers.Container {
	t.Helper()
	ctx := context.Background()

	if cfg.Image == "" {
		cfg.Image = testContainerImage
	}

	cr := testcontainers.ContainerRequest{
		Image: cfg.Image,
		Labels: cfg.Labels,
		Cmd:    cfg.Cmd,
	}

	for _, p := range cfg.ExposedPorts {
		cr.ExposedPorts = append(cr.ExposedPorts, p)
	}

	if cfg.NetworkMode != "" {
		cr.NetworkMode = ctypes.NetworkMode(cfg.NetworkMode)
	}

	cr.Networks = append(cr.Networks, cfg.Networks...)

	for k, v := range cfg.Env {
		if cr.Env == nil {
			cr.Env = map[string]string{}
		}
		cr.Env[k] = v
	}

	opts := []testcontainers.ContainerCustomizer{
		testcontainers.CustomizeRequest(testcontainers.GenericContainerRequest{
			ContainerRequest: cr,
			Started: true,
		}),
	}

	if cfg.WaitPort != "skip" {
		waitPort := cfg.WaitPort
		if waitPort == "" {
			waitPort = "80/tcp"
		}
		opts = append(opts, testcontainers.WithWaitStrategy(
			wait.ForListeningPort(waitPort).WithStartupTimeout(30*time.Second),
		))
	}

	ctr, err := testcontainers.Run(ctx, cfg.Image, opts...)
	require.NoError(t, err, "failed to start container")
	testcontainers.CleanupContainer(t, ctr)

	return ctr
}

// --- tsnet Test Client ---

type TSNetClient struct {
	server  *tsnet.Server
	MagicDNSSuffix string
}

func NewTSNetClient(t *testing.T, authKey string) *TSNetClient {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), tsnetStartupTimeout)
	defer cancel()

	server := &tsnet.Server{
		Dir:       filepath.Join(e2eBaseDir, t.Name(), "tsclient"),
		Hostname:  fmt.Sprintf("e2e-test-client-%d", time.Now().UnixNano()),
		AuthKey:   authKey,
		Ephemeral: true,
		Logf: func(format string, args ...any) {
			t.Logf("[tsnet-client] "+format, args...)
		},
	}
	t.Cleanup(func() { server.Close() })

	status, err := server.Up(ctx)
	require.NoError(t, err, "tsnet client failed to start")
	require.NotNil(t, status.Self, "tsnet client has no self node")

	return &TSNetClient{
		server:         server,
		MagicDNSSuffix: status.CurrentTailnet.MagicDNSSuffix,
	}
}

func (tc *TSNetClient) Dial(ctx context.Context, network, addr string) (net.Conn, error) {
	return tc.server.Dial(ctx, network, addr)

}

// dialHost returns the host:port string from a URL, adding the default port
// for the scheme if missing (url.Parse omits default ports from URL.Host).
func dialHost(u *url.URL) string {
	if _, _, err := net.SplitHostPort(u.Host); err == nil {
		return u.Host
	}
	switch u.Scheme {
	case "https":
		return u.Host + ":443"
	default:
		return u.Host + ":80"
	}
}

// GetNoFollowRedirect makes a request via tsnet but does not follow redirects.
// Returns the initial response (which may be a 3xx). Uses TLS.
func (tc *TSNetClient) GetNoFollowRedirect(ctx context.Context, targetURL string) (*http.Response, error) {
	return tc.getNoFollowRedirect(ctx, targetURL, true)
}

// GetNoFollowRedirectHTTP makes a plain HTTP request via tsnet without
// following redirects. Use for non-TLS endpoints like redirect ports.
func (tc *TSNetClient) GetNoFollowRedirectHTTP(ctx context.Context, targetURL string) (*http.Response, error) {
	return tc.getNoFollowRedirect(ctx, targetURL, false)
}

func (tc *TSNetClient) getNoFollowRedirect(ctx context.Context, targetURL string, useTLS bool) (*http.Response, error) {
	parsed, err := url.Parse(targetURL)
	if err != nil {
		return nil, fmt.Errorf("parse URL: %w", err)
	}

	addr := dialHost(parsed)
	conn, err := tc.server.Dial(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", addr, err)
	}

	var rw io.ReadWriter = conn
	if useTLS {
		tlsConn := tls.Client(conn, &tls.Config{
			ServerName:         parsed.Hostname(),
			InsecureSkipVerify: true,
		})
		if err := tlsConn.Handshake(); err != nil {
			conn.Close()
			return nil, fmt.Errorf("TLS handshake: %w", err)
		}
		rw = tlsConn
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
	if err != nil {
		conn.Close()
		return nil, err
	}

	req.Header.Set("Connection", "close")

	if err := req.Write(rw); err != nil {
		conn.Close()
		return nil, fmt.Errorf("write request: %w", err)
	}

	resp, err := http.ReadResponse(bufio.NewReader(rw), req)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("read response: %w", err)
	}

	resp.Body = &connCloser{body: resp.Body, conn: conn}
	return resp, nil
}

// connCloser wraps an io.ReadCloser and also closes the underlying net.Conn.
type connCloser struct {
	body io.ReadCloser
	conn net.Conn
}

func (cc *connCloser) Read(p []byte) (int, error) { return cc.body.Read(p) }
func (cc *connCloser) Close() error {
	closeErr := cc.body.Close()
	connErr := cc.conn.Close()
	if closeErr != nil {
		return closeErr
	}
	return connErr
}

func (tc *TSNetClient) Get(ctx context.Context, targetURL string) (*http.Response, error) {
	parsed, err := url.Parse(targetURL)
	if err != nil {
		return nil, fmt.Errorf("parse URL: %w", err)
	}

	addr := dialHost(parsed)
	conn, err := tc.server.Dial(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", addr, err)
	}

	var rw io.ReadWriter = conn

	if parsed.Scheme == "https" {
		host := parsed.Hostname()
		tlsConn := tls.Client(conn, &tls.Config{
			ServerName:       host,
			InsecureSkipVerify: true,
		})
		if err := tlsConn.Handshake(); err != nil {
			conn.Close()
			return nil, fmt.Errorf("TLS handshake: %w", err)
		}
		rw = tlsConn
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
	if err != nil {
		conn.Close()
		return nil, err
	}

	if err := req.Write(rw); err != nil {
		conn.Close()
		return nil, fmt.Errorf("write request: %w", err)
	}

	resp, err := http.ReadResponse(bufio.NewReader(rw), req)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("read response: %w", err)
	}

	resp.Body = &connCloser{body: resp.Body, conn: conn}
	return resp, nil
}

func (tc *TSNetClient) ProxyURL(hostname string) string {
	return fmt.Sprintf("https://%s.%s", hostname, tc.MagicDNSSuffix)
}

func (tc *TSNetClient) ProxyHTTPURL(hostname string) string {
	return fmt.Sprintf("http://%s.%s", hostname, tc.MagicDNSSuffix)
}

func (tc *TSNetClient) ProxyTCPAddress(hostname string, port int) string {
	return fmt.Sprintf("%s.%s:%d", hostname, tc.MagicDNSSuffix, port)
}

func mustURLPort(t *testing.T, rawURL string) string {
	t.Helper()
	parsed, err := url.Parse(rawURL)
	require.NoError(t, err, "failed to parse URL %q", rawURL)
	port := parsed.Port()
	require.NotEmpty(t, port, "URL %q has no port", rawURL)
	return port
}

func (tc *TSNetClient) GetPeerByDNSName(ctx context.Context, hostname string) (*ipnstate.PeerStatus, error) {
	lc, err := tc.server.LocalClient()
	if err != nil {
		return nil, fmt.Errorf("get local client: %w", err)
	}

	status, err := lc.Status(ctx)
	if err != nil {
		return nil, fmt.Errorf("get status: %w", err)
	}

	fqdn := hostname + "." + tc.MagicDNSSuffix + "."
	for _, peer := range status.Peer {
		if peer.DNSName == fqdn {
			return peer, nil
		}
	}

	return nil, fmt.Errorf("peer %s not found (have %d peers)", fqdn, len(status.Peer))
}

func waitForPeerByDNSName(t *testing.T, ctx context.Context, client *TSNetClient, hostname string, timeout time.Duration) *ipnstate.PeerStatus {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			t.Fatalf("context cancelled waiting for peer %s: %v", hostname, ctx.Err())
		default:
		}

		peer, err := client.GetPeerByDNSName(ctx, hostname)
		if err == nil {
			return peer
		}
		time.Sleep(2 * time.Second)
	}
	t.Fatalf("timeout waiting for peer %s to appear in tailnet", hostname)
	return nil
}

// --- Self-Signed HTTPS Server ---

func StartSelfSignedHTTPSServer(t *testing.T, handler http.Handler) *httptest.Server {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err, "failed to generate ECDSA key")

	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "e2e-test"},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(1 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{"localhost"},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	require.NoError(t, err, "failed to create certificate")

	tlsCert := tls.Certificate{
		Certificate: [][]byte{certDER},
		PrivateKey:  key,
	}

	srv := httptest.NewUnstartedServer(handler)
	srv.TLS = &tls.Config{
		Certificates: []tls.Certificate{tlsCert},
	}
	srv.StartTLS()

	t.Cleanup(srv.Close)

	return srv
}

func StartSelfSignedHTTPSContainer(t *testing.T, labels map[string]string) testcontainers.Container {
	t.Helper()

	cmd := []string{
		"sh",
		"-ec",
		`apk add --no-cache openssl >/dev/null
mkdir -p /srv
printf 'HTTPS OK' > /srv/index.html
openssl req -x509 -newkey rsa:2048 -nodes -keyout /tmp/key.pem -out /tmp/cert.pem -subj '/CN=localhost' -days 1 >/dev/null 2>&1
python - <<'PY'
import functools
import http.server
import socketserver
import ssl

class Handler(http.server.SimpleHTTPRequestHandler):
    def log_message(self, *args):
        pass

handler = functools.partial(Handler, directory='/srv')
httpd = socketserver.TCPServer(('0.0.0.0', 8443), handler)
ctx = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
ctx.load_cert_chain('/tmp/cert.pem', '/tmp/key.pem')
httpd.socket = ctx.wrap_socket(httpd.socket, server_side=True)
httpd.serve_forever()
PY`,
	}

	return StartContainer(t, ContainerConfig{
		Image:        "python:3.12-alpine",
		Cmd:          cmd,
		Labels:       labels,
		ExposedPorts: []string{"8443/tcp"},
		WaitPort:     "8443/tcp",
	})
}

// --- Verification Helpers ---

func WaitForProxyReachable(t *testing.T, ctx context.Context, client *TSNetClient, proxyURL string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			t.Fatalf("context cancelled waiting for proxy %s: %v", proxyURL, ctx.Err())
		default:
		}

		resp, err := client.Get(ctx, proxyURL)
		if err == nil {
			resp.Body.Close()
			return
		}
		time.Sleep(2 * time.Second)
	}

	t.Fatalf("timeout waiting for proxy to be reachable at %s", proxyURL)
}

func VerifyHTTPResponse(t *testing.T, ctx context.Context, client *TSNetClient, proxyURL, expectedSubstring string) {
	t.Helper()

	resp, err := client.Get(ctx, proxyURL)
	require.NoError(t, err, "failed to GET %s", proxyURL)
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err, "failed to read response body")

	require.Equal(t, http.StatusOK, resp.StatusCode,
		"unexpected status code from %s: body=%s", proxyURL, string(body))
	require.Contains(t, string(body), expectedSubstring,
		"response from %s does not contain expected substring", proxyURL)
}

// --- Config Generation ---

type configParams struct {
	HTTPPort       int
	AuthKey        string
	AuthKeyFile    string
	Tags           string
	DataDir        string
	DockerHost     string
	TargetHostname string
	Ephemeral      bool
	ControlURL     string
	ClientID       string
	ClientSecret   string
}

func generateConfig(p configParams) string {
	if p.HTTPPort == 0 {
		p.HTTPPort = 8080
	}
	if p.DataDir == "" {
		p.DataDir = "/tmp/tsdproxy-e2e-data"
	}
	if p.TargetHostname == "" {
		p.TargetHostname = "172.17.0.1"
	}

	var authKeyLine string
	switch {
	case p.AuthKeyFile != "":
		authKeyLine = fmt.Sprintf("      authKeyFile: %q", p.AuthKeyFile)
	case p.AuthKey != "":
		authKeyLine = fmt.Sprintf("      authKey: %q", p.AuthKey)
	}

	var tagsLine string
	if p.Tags != "" {
		tagsLine = fmt.Sprintf("\n      tags: %q", p.Tags)
	}

	var controlURLLine string
	if p.ControlURL != "" {
		controlURLLine = fmt.Sprintf("\n      controlUrl: %q", p.ControlURL)
	}

	var clientLines string
	if p.ClientID != "" {
		clientLines = fmt.Sprintf("\n      clientId: %q\n      clientSecret: %q", p.ClientID, p.ClientSecret)
	}

	var dockerHostLine string
	if p.DockerHost != "" {
		dockerHostLine = fmt.Sprintf("    host: %q\n", p.DockerHost)
	}

	return fmt.Sprintf(`defaultProxyProvider: default
docker:
  local:
%s    targetHostname: %q
tailscale:
  providers:
    default:
%s%s%s%s
  dataDir: %q
http:
  hostname: "0.0.0.0"
  port: %d
log:
  level: debug
  json: false
proxyAccessLog: true
`,
		dockerHostLine,
		p.TargetHostname,
		authKeyLine,
		tagsLine,
		controlURLLine,
		clientLines,
		p.DataDir,
		p.HTTPPort,
	)
}

// GenerateListProviderFile creates a YAML file for the list target provider.
func GenerateListProviderFile(t *testing.T, entries map[string]ListEntry) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "list.yaml")
	WriteListProviderFile(t, path, entries)
	return path
}


func RenderListProviderFile(entries map[string]ListEntry) string {
	var sb strings.Builder
	for name, entry := range entries {
		sb.WriteString(fmt.Sprintf("%s:\n", name))
		sb.WriteString(fmt.Sprintf("  proxyProvider: %q\n", entry.ProxyProvider))
		if entry.Tailscale.Tags != "" || entry.Tailscale.Ephemeral {
			sb.WriteString("  tailscale:\n")
			if entry.Tailscale.Tags != "" {
				sb.WriteString(fmt.Sprintf("    tags: %q\n", entry.Tailscale.Tags))
			}
			if entry.Tailscale.Ephemeral {
				sb.WriteString("    ephemeral: true\n")
			}
		}
		sb.WriteString("  dashboard:\n")
		sb.WriteString(fmt.Sprintf("    visible: %v\n", entry.Dashboard.Visible))
		if entry.Dashboard.Label != "" {
			sb.WriteString(fmt.Sprintf("    label: %q\n", entry.Dashboard.Label))
		}
		if entry.Dashboard.Icon != "" {
			sb.WriteString(fmt.Sprintf("    icon: %q\n", entry.Dashboard.Icon))
		}
		sb.WriteString("  ports:\n")
		for portName, port := range entry.Ports {
			sb.WriteString(fmt.Sprintf("    %q:\n", portName))
			sb.WriteString("      targets:\n")
			for _, target := range port.Targets {
				sb.WriteString(fmt.Sprintf("        - %q\n", target))
			}
			if port.TLSValidate {
				sb.WriteString("      tlsValidate: true\n")
			}
			if port.Funnel {
				sb.WriteString("      tailscale:\n        funnel: true\n")
			}
		}
	}
	return sb.String()
}

func WriteListProviderFile(t *testing.T, path string, entries map[string]ListEntry) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, []byte(RenderListProviderFile(entries)), 0o644))
}

type ListEntry struct {
	ProxyProvider string
	Tailscale     ListTailscale
	Dashboard     ListDashboard
	Ports         map[string]ListPort
}

type ListTailscale struct {
	Tags      string
	Ephemeral bool
}

type ListDashboard struct {
	Visible bool
	Label   string
	Icon    string
}

type ListPort struct {
	Targets      []string
	TLSValidate  bool
	Funnel       bool
}

// --- Utility ---

func e2eTestDataDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(e2eBaseDir, t.Name())
	require.NoError(t, os.MkdirAll(dir, 0o755))
	return dir
}

// getFreePort returns a currently available TCP port.
//
// NOTE: There is a TOCTOU race — the port may be claimed between the
// listener close and the actual bind. This is acceptable for e2e tests
// which run sequentially (no t.Parallel()). If flakiness appears,
// consider using net.Listen directly and passing the listener.
func getFreePort() int {
	addr, err := net.ResolveTCPAddr("tcp", "localhost:0")
	if err != nil {
		panic(err)
	}
	ln, err := net.ListenTCP("tcp", addr)
	if err != nil {
		panic(err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

type testLogWriter struct {
	t      *testing.T
	prefix string
}

func (w *testLogWriter) Write(p []byte) (n int, err error) {
	w.t.Logf("%s%s", w.prefix, string(p))
	return len(p), nil
}
