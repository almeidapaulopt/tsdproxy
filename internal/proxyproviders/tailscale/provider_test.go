// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package tailscale

import (
	"context"
	"testing"

	"github.com/rs/zerolog"
	"golang.org/x/sync/semaphore"

	"github.com/almeidapaulopt/tsdproxy/internal/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateDatadir(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		baseDir  string
		hostname string
		wantPath string
		wantErr  bool
	}{
		// Valid cases
		{name: "simple name", baseDir: "data", hostname: "myservice", wantErr: false, wantPath: "data/myservice"},
		{name: "name with hyphen", baseDir: "data", hostname: "my-service", wantErr: false, wantPath: "data/my-service"},
		{name: "nested path subdirectory", baseDir: "data", hostname: "nested/path", wantErr: false, wantPath: "data/nested/path"},
		{name: "single character", baseDir: "data", hostname: "a", wantErr: false, wantPath: "data/a"},
		{name: "numbers in name", baseDir: "data", hostname: "service123", wantErr: false, wantPath: "data/service123"},
		{name: "absolute baseDir", baseDir: "/var/lib/tsdproxy", hostname: "myapp", wantErr: false, wantPath: "/var/lib/tsdproxy/myapp"},
		{name: "trailing slash on baseDir", baseDir: "data/", hostname: "myapp", wantErr: false, wantPath: "data/myapp"},
		{name: "base dir with relative components", baseDir: "data/../data", hostname: "myapp", wantErr: false, wantPath: "data/myapp"},

		// Path traversal attacks (must fail)
		{name: "parent traversal", baseDir: "data", hostname: "../../etc/passwd", wantErr: true},
		{name: "single parent", baseDir: "data", hostname: "..", wantErr: true},
		{name: "double parent", baseDir: "data", hostname: "../..", wantErr: true},
		{name: "nested traversal", baseDir: "data", hostname: "valid/../../../etc", wantErr: true},

		// Edge cases
		{
			name: "long hostname", baseDir: "data",
			hostname: "abcdefghijklmnopqrstuvwxyzabcdefghijklmnopqrstuvwxyz0123456789abcdefghijklmnopqrstuvwxyz",
			wantErr:  false,
			wantPath: "data/abcdefghijklmnopqrstuvwxyzabcdefghijklmnopqrstuvwxyz0123456789abcdefghijklmnopqrstuvwxyz",
		},
		{name: "unicode hostname", baseDir: "data", hostname: "serviço", wantErr: false, wantPath: "data/serviço"},
		{name: "empty baseDir", baseDir: "", hostname: "myapp", wantErr: false, wantPath: "myapp"},
		{name: "empty both", baseDir: "", hostname: "", wantErr: false, wantPath: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := validateDatadir(tt.baseDir, tt.hostname)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "path escaping",
					"error should mention path escaping for hostname %q", tt.hostname)
				return
			}
			require.NoError(t, err, "hostname: %q, baseDir: %q", tt.hostname, tt.baseDir)
			assert.Equal(t, tt.wantPath, got)
		})
	}
}

// SECURITY: hostile hostname must not escape the data directory.
func TestValidateDatadir_Security(t *testing.T) {
	t.Parallel()

	hostileHostnames := []struct {
		name    string
		host    string
		wantMsg string
	}{
		{name: "parent traversal", host: "../../etc/passwd", wantMsg: "results in path escaping data directory"},
		{name: "single parent", host: "..", wantMsg: "results in path escaping data directory"},
		{name: "double parent", host: "../..", wantMsg: "results in path escaping data directory"},
		{name: "nested traversal", host: "valid/../../../etc", wantMsg: "results in path escaping data directory"},
		{name: "absolute path", host: "/etc/passwd", wantMsg: "is an absolute path"},
		{name: "null byte injection", host: "test\x00../../etc", wantMsg: "contains null byte"},
	}

	for _, tc := range hostileHostnames {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := validateDatadir("data", tc.host)
			require.Error(t, err, "hostname %q should be rejected", tc.host)
			assert.Contains(t, err.Error(), tc.wantMsg,
				"hostname %q: error %q must contain %q", tc.host, err.Error(), tc.wantMsg)
		})
	}
}

func TestIsDomainRequired(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		shared   bool
		services bool
		want     bool
	}{
		{name: "shared mode", shared: true, services: false, want: true},
		{name: "not shared", shared: false, services: false, want: false},
		{name: "services mode (not shared)", shared: false, services: true, want: false},
		{name: "both modes", shared: true, services: true, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c := &Client{shared: tt.shared, services: tt.services}
			assert.Equal(t, tt.want, c.IsDomainRequired())
		})
	}
}

func TestGetControlURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		controlURL string
		want       string
	}{
		{name: "empty falls back to default", controlURL: "", want: model.DefaultTailscaleControlURL},
		{name: "custom URL returned", controlURL: "https://custom.tailscale.com", want: "https://custom.tailscale.com"},
		{name: "standard control plane", controlURL: "https://controlplane.tailscale.com", want: "https://controlplane.tailscale.com"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c := &Client{controlURL: tt.controlURL}
			assert.Equal(t, tt.want, c.getControlURL())
		})
	}
}

func TestResolveTags(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		clientTags string
		cfgTags    string
		want       string
	}{
		{name: "proxy tags take priority", clientTags: "tag:provider", cfgTags: "tag:proxy", want: "tag:proxy"},
		{name: "falls back to provider tags", clientTags: "tag:provider", cfgTags: "", want: "tag:provider"},
		{name: "both empty returns empty", clientTags: "", cfgTags: "", want: ""},
		{name: "proxy tags with quotes stripped", clientTags: "tag:p", cfgTags: `"tag:proxy"`, want: "tag:proxy"},
		{name: "provider tags with quotes stripped", clientTags: `"tag:provider"`, cfgTags: "", want: "tag:provider"},
		{name: "whitespace trimmed", clientTags: "  tag:p  ", cfgTags: " tag:proxy ", want: "tag:proxy"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c := &Client{tags: tt.clientTags}
			cfg := &model.Config{Tailscale: model.Tailscale{Tags: tt.cfgTags}}
			assert.Equal(t, tt.want, c.resolveTags(cfg))
		})
	}
}

func TestClose_NilServers(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	c := &Client{
		providerCtx:    ctx,
		providerCancel: cancel,
	}
	assert.NotPanics(t, func() { c.Close() })
	assert.ErrorIs(t, c.providerCtx.Err(), context.Canceled)
}

func TestNewSharedProxy_RequiresDomain(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := &Client{
		shared:         true,
		sharedHostname: "shared-host",
		datadir:        t.TempDir(),
		providerCtx:    ctx,
		providerCancel: cancel,
		log:            zerolog.Nop(),
	}
	t.Cleanup(func() {
		c.sharedMu.Lock()
		if c.sharedServer != nil {
			c.sharedServer.Close()
		}
		c.sharedMu.Unlock()
	})
	cfg := &model.Config{
		Hostname: "my-proxy",
	}
	_, err := c.newSharedProxy(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "domain")
}

func TestNewServiceProxy_RejectsDomain(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := &Client{
		services:       true,
		sharedHostname: "services-host",
		datadir:        t.TempDir(),
		providerCtx:    ctx,
		providerCancel: cancel,
		log:            zerolog.Nop(),
	}
	cfg := &model.Config{
		Hostname: "my-proxy",
		Domain:   "custom.example.com",
	}
	_, err := c.newServiceProxy(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not support custom domains")
}

func TestNewProxy_ModeDispatch(t *testing.T) {
	t.Run("shared mode dispatches to newSharedProxy", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		c := &Client{
			shared:         true,
			sharedHostname: "shared-host",
			datadir:        t.TempDir(),
			providerCtx:    ctx,
			providerCancel: cancel,
			log:            zerolog.Nop(),
		}
		t.Cleanup(func() {
			c.sharedMu.Lock()
			if c.sharedServer != nil {
				c.sharedServer.Close()
			}
			c.sharedMu.Unlock()
		})
		cfg := &model.Config{Hostname: "test"}
		_, err := c.NewProxy(cfg)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "domain")
	})

	t.Run("services mode dispatches to newServiceProxy", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		c := &Client{
			services:       true,
			sharedHostname: "services-host",
			datadir:        t.TempDir(),
			providerCtx:    ctx,
			providerCancel: cancel,
			log:            zerolog.Nop(),
		}
		cfg := &model.Config{
			Hostname: "test",
			Domain:   "should-fail.example.com",
		}
		_, err := c.NewProxy(cfg)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "does not support custom domains")
	})

	t.Run("per-proxy mode returns non-nil proxy", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		certSem := semaphore.NewWeighted(2) //nolint:mnd
		c := &Client{
			shared:         false,
			services:       false,
			datadir:        t.TempDir(),
			providerCtx:    ctx,
			providerCancel: cancel,
			log:            zerolog.Nop(),
			certSem:        certSem,
		}
		cfg := &model.Config{Hostname: "my-proxy"}
		proxy, err := c.NewProxy(cfg)
		require.NoError(t, err)
		require.NotNil(t, proxy)
	})
}
