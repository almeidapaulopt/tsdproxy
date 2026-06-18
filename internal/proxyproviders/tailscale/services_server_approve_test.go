// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package tailscale

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"tailscale.com/client/tailscale/v2"
	"tailscale.com/ipn/ipnstate"
	"tailscale.com/tailcfg"
	"tailscale.com/tsnet"

	"github.com/almeidapaulopt/tsdproxy/internal/core/secretstring"
)

func TestApproveServiceDevice_NilTSServer_ReturnsNil(t *testing.T) {
	t.Parallel()

	ss := NewServicesServer(ServicesServerConfig{
		Hostname: "test",
		Log:      zerolog.Nop(),
	})
	defer ss.Close()

	err := ss.approveServiceDeviceForServer(context.Background(), nil, "svc:test")
	assert.NoError(t, err, "nil tsServer should return nil without error")
}

func TestApproveServiceDevice_NilAPIClient_ReturnsError(t *testing.T) {
	t.Parallel()

	ss := NewServicesServer(ServicesServerConfig{
		Hostname: "test",
		Log:      zerolog.Nop(),
	})
	defer ss.Close()

	tsServer := &tsnet.Server{}
	err := ss.approveServiceDeviceForServer(context.Background(), tsServer, "svc:test")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "tailscale API client not configured")
}

func TestApproveServiceDevice_Success(t *testing.T) {
	t.Parallel()

	var gotMethod, gotPath, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := testTailscaleClient(srv.URL)

	ss := NewServicesServer(ServicesServerConfig{
		Hostname:  "test",
		Log:       zerolog.Nop(),
		APIClient: client,
	})
	defer ss.Close()

	ss.getNodeStatusFunc = func(_ context.Context) (*ipnstate.Status, error) {
		return &ipnstate.Status{
			Self: &ipnstate.PeerStatus{ID: tailcfg.StableNodeID("42")},
		}, nil
	}

	tsServer := &tsnet.Server{}
	err := ss.approveServiceDeviceForServer(context.Background(), tsServer, "svc:myapp")
	require.NoError(t, err)

	assert.Equal(t, http.MethodPost, gotMethod)
	assert.Equal(t, "/api/v2/tailnet/-/services/svc:myapp/device/42/approved", gotPath)
	assert.Equal(t, `{"approved":true}`, gotBody)
}

func TestApproveServiceDevice_APINon200_ReturnsError(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":"forbidden"}`))
	}))
	defer srv.Close()

	client := testTailscaleClient(srv.URL)

	ss := NewServicesServer(ServicesServerConfig{
		Hostname:  "test",
		Log:       zerolog.Nop(),
		APIClient: client,
	})
	defer ss.Close()

	ss.getNodeStatusFunc = func(_ context.Context) (*ipnstate.Status, error) {
		return &ipnstate.Status{
			Self: &ipnstate.PeerStatus{ID: tailcfg.StableNodeID("1")},
		}, nil
	}

	tsServer := &tsnet.Server{}
	err := ss.approveServiceDeviceForServer(context.Background(), tsServer, "svc:test")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "approval failed (HTTP 403)")
	assert.Contains(t, err.Error(), `{"error":"forbidden"}`)
}

func TestApproveServiceDevice_GetNodeStatusFails_ReturnsError(t *testing.T) {
	t.Parallel()

	client := testTailscaleClient("http://unused.example.com")

	ss := NewServicesServer(ServicesServerConfig{
		Hostname:  "test",
		Log:       zerolog.Nop(),
		APIClient: client,
	})
	defer ss.Close()

	statusErr := errors.New("local client unavailable")
	ss.getNodeStatusFunc = func(_ context.Context) (*ipnstate.Status, error) {
		return nil, statusErr
	}

	tsServer := &tsnet.Server{}
	err := ss.approveServiceDeviceForServer(context.Background(), tsServer, "svc:test")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "get node status")
	assert.ErrorIs(t, err, statusErr)
}

func TestApproveServiceDevice_SelfNil_ReturnsError(t *testing.T) {
	t.Parallel()

	client := testTailscaleClient("http://unused.example.com")

	ss := NewServicesServer(ServicesServerConfig{
		Hostname:  "test",
		Log:       zerolog.Nop(),
		APIClient: client,
	})
	defer ss.Close()

	ss.getNodeStatusFunc = func(_ context.Context) (*ipnstate.Status, error) {
		return &ipnstate.Status{Self: nil}, nil
	}

	tsServer := &tsnet.Server{}
	err := ss.approveServiceDeviceForServer(context.Background(), tsServer, "svc:test")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no self in node status")
}

func TestApproveServiceDevice_EmptyNodeID_ReturnsError(t *testing.T) {
	t.Parallel()

	client := testTailscaleClient("http://unused.example.com")

	ss := NewServicesServer(ServicesServerConfig{
		Hostname:  "test",
		Log:       zerolog.Nop(),
		APIClient: client,
	})
	defer ss.Close()

	ss.getNodeStatusFunc = func(_ context.Context) (*ipnstate.Status, error) {
		return &ipnstate.Status{
			Self: &ipnstate.PeerStatus{ID: tailcfg.StableNodeID("")},
		}, nil
	}

	tsServer := &tsnet.Server{}
	err := ss.approveServiceDeviceForServer(context.Background(), tsServer, "svc:test")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty node ID in status")
}

func TestApproveServiceDevice_RequestURLContainsCorrectServiceAndNode(t *testing.T) {
	t.Parallel()

	var capturedPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := testTailscaleClient(srv.URL)

	ss := NewServicesServer(ServicesServerConfig{
		Hostname:  "test",
		Log:       zerolog.Nop(),
		APIClient: client,
	})
	defer ss.Close()

	ss.getNodeStatusFunc = func(_ context.Context) (*ipnstate.Status, error) {
		return &ipnstate.Status{
			Self: &ipnstate.PeerStatus{ID: tailcfg.StableNodeID("99")},
		}, nil
	}

	tsServer := &tsnet.Server{}
	err := ss.approveServiceDeviceForServer(context.Background(), tsServer, "svc:custom-name")
	require.NoError(t, err)

	expected := "/api/v2/tailnet/-/services/svc:custom-name/device/99/approved"
	assert.Equal(t, expected, capturedPath)
}

func TestApproveServiceDevice_RequestBodyIsJSON(t *testing.T) {
	t.Parallel()

	var gotContentType string
	var bodyBytes []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotContentType = r.Header.Get("Content-Type")
		bodyBytes, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := testTailscaleClient(srv.URL)

	ss := NewServicesServer(ServicesServerConfig{
		Hostname:  "test",
		Log:       zerolog.Nop(),
		APIClient: client,
	})
	defer ss.Close()

	ss.getNodeStatusFunc = func(_ context.Context) (*ipnstate.Status, error) {
		return &ipnstate.Status{
			Self: &ipnstate.PeerStatus{ID: tailcfg.StableNodeID("7")},
		}, nil
	}

	tsServer := &tsnet.Server{}
	err := ss.approveServiceDeviceForServer(context.Background(), tsServer, "svc:x")
	require.NoError(t, err)

	assert.Equal(t, "application/json", gotContentType)

	var parsed map[string]bool
	require.NoError(t, json.Unmarshal(bodyBytes, &parsed))
	assert.True(t, parsed["approved"])
}

// testTailscaleClient creates a *tailscale.Client whose BaseURL and HTTP
// client are wired to the given test server URL.
func testTailscaleClient(serverURL string) *tailscale.Client {
	u, _ := url.Parse(serverURL)
	return &tailscale.Client{
		BaseURL: u,
		HTTP:    &http.Client{},
	}
}

// testProductionLikeClient creates a *tailscale.Client exactly like
// APIClientFactory.NewClient does: no BaseURL, no HTTP — only Tailnet and Auth.
// This reproduces the state that caused the original nil-panic bug.
func testProductionLikeClient() *tailscale.Client {
	return &tailscale.Client{
		Tailnet:   "-",
		UserAgent: "test",
		Auth: &tailscale.OAuth{
			ClientID:     "test-id",
			ClientSecret: "test-secret",
			Scopes:       ScopesServices(),
		},
	}
}

func TestApproveServiceDevice_ProductionClientNoPanic(t *testing.T) {
	t.Parallel()

	client := testProductionLikeClient()

	ss := NewServicesServer(ServicesServerConfig{
		Hostname:  "test",
		Log:       zerolog.Nop(),
		APIClient: client,
	})
	defer ss.Close()

	ss.getNodeStatusFunc = func(_ context.Context) (*ipnstate.Status, error) {
		return &ipnstate.Status{
			Self: &ipnstate.PeerStatus{ID: tailcfg.StableNodeID("1")},
		}, nil
	}

	tsServer := &tsnet.Server{}

	// The function must not panic even though client.BaseURL and client.HTTP
	// are nil (the lazy init via VIPServices() should populate them).
	// The HTTP call will fail since there's no real server, but that's expected.
	err := ss.approveServiceDeviceForServer(context.Background(), tsServer, "svc:test")
	require.Error(t, err, "should return an error (no real API server), but must not panic")
}

func TestApproveServiceDevice_APIFactoryClientNoPanic(t *testing.T) {
	t.Parallel()

	// This is the actual production code path: ServicesServer is configured with
	// APIFactory, not APIClient. getAPIClient() calls apiFactory.NewClient() which
	// returns a client without BaseURL/HTTP. The lazy init via VIPServices() must
	// populate them without panicking.
	factory := NewAPIClientFactory("test-id", secretstring.SecretString("test-secret"))

	ss := NewServicesServer(ServicesServerConfig{
		Hostname:   "test",
		Log:        zerolog.Nop(),
		APIFactory: factory,
	})
	defer ss.Close()

	ss.getNodeStatusFunc = func(_ context.Context) (*ipnstate.Status, error) {
		return &ipnstate.Status{
			Self: &ipnstate.PeerStatus{ID: tailcfg.StableNodeID("1")},
		}, nil
	}

	tsServer := &tsnet.Server{}

	// The HTTP call will fail (OAuth to real api.tailscale.com with fake creds),
	// but it must NOT panic due to nil BaseURL.
	err := ss.approveServiceDeviceForServer(context.Background(), tsServer, "svc:test")
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "nil pointer dereference")
	assert.NotContains(t, err.Error(), "invalid memory address")
}

func TestApproveServiceDevice_APIFactoryUnavailable_ReturnsError(t *testing.T) {
	t.Parallel()

	// Empty credentials → factory is unavailable → getAPIClient returns nil.
	factory := NewAPIClientFactory("", secretstring.SecretString(""))

	ss := NewServicesServer(ServicesServerConfig{
		Hostname:   "test",
		Log:        zerolog.Nop(),
		APIFactory: factory,
	})
	defer ss.Close()

	tsServer := &tsnet.Server{}
	err := ss.approveServiceDeviceForServer(context.Background(), tsServer, "svc:test")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "tailscale API client not configured")
}

func TestApproveServiceDevice_CancelledContext(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		time.Sleep(5 * time.Second)
	}))
	defer srv.Close()

	client := testTailscaleClient(srv.URL)

	ss := NewServicesServer(ServicesServerConfig{
		Hostname:  "test",
		Log:       zerolog.Nop(),
		APIClient: client,
	})
	defer ss.Close()

	ss.getNodeStatusFunc = func(_ context.Context) (*ipnstate.Status, error) {
		return &ipnstate.Status{
			Self: &ipnstate.PeerStatus{ID: tailcfg.StableNodeID("1")},
		}, nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	tsServer := &tsnet.Server{}
	err := ss.approveServiceDeviceForServer(ctx, tsServer, "svc:test")
	require.Error(t, err)
}

func TestApproveServiceDevice_ResponseReadErrorOnNon200(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		// Close connection mid-body to force a read error.
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	}))
	defer srv.Close()

	client := testTailscaleClient(srv.URL)

	ss := NewServicesServer(ServicesServerConfig{
		Hostname:  "test",
		Log:       zerolog.Nop(),
		APIClient: client,
	})
	defer ss.Close()

	ss.getNodeStatusFunc = func(_ context.Context) (*ipnstate.Status, error) {
		return &ipnstate.Status{
			Self: &ipnstate.PeerStatus{ID: tailcfg.StableNodeID("1")},
		}, nil
	}

	tsServer := &tsnet.Server{}
	err := ss.approveServiceDeviceForServer(context.Background(), tsServer, "svc:test")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "approval failed (HTTP 500)")
}

func TestAcquireWithAutoApprove_FailureDoesNotBlockListener(t *testing.T) {
	t.Parallel()

	vip := &mockVIPAPI{}
	factory := defaultFactory()

	// Create server with autoApproveDevices=true but no API client configured.
	// Approval will fail but the acquire should still succeed.
	ss := NewServicesServer(ServicesServerConfig{
		Hostname:           "test",
		Log:                zerolog.Nop(),
		VIPServiceAPI:      vip,
		AutoApproveDevices: true,
		LifecycleConfig:    &NodeLifecycleConfig{},
		LifecycleProvider: func(_ context.Context, _ zerolog.Logger, _ NodeLifecycleConfig) (
			*NodeLifecycle, *NodeRuntime, serviceListenerFactory, error,
		) {
			ctx, cancel := context.WithCancel(context.Background())
			nodeRt := &NodeRuntime{
				Ctx:    ctx,
				Cancel: cancel,
			}
			nl := &NodeLifecycle{
				events: make(chan NodeEvent),
			}
			return nl, nodeRt, factory, nil
		},
	})
	defer ss.Close()

	sl, err := ss.Acquire("svc:test", 443, true, false)
	require.NoError(t, err, "acquire should succeed even when approval fails")
	require.NotNil(t, sl, "listener should be returned even when approval fails")
}

func TestAcquireWithoutAutoApprove_DoesNotCallApproval(t *testing.T) {
	t.Parallel()

	vip := &mockVIPAPI{}
	factory := defaultFactory()

	ss := NewServicesServer(ServicesServerConfig{
		Hostname:           "test",
		Log:                zerolog.Nop(),
		VIPServiceAPI:      vip,
		AutoApproveDevices: false,
		LifecycleConfig:    &NodeLifecycleConfig{},
		LifecycleProvider: func(_ context.Context, _ zerolog.Logger, _ NodeLifecycleConfig) (
			*NodeLifecycle, *NodeRuntime, serviceListenerFactory, error,
		) {
			ctx, cancel := context.WithCancel(context.Background())
			nodeRt := &NodeRuntime{
				Ctx:    ctx,
				Cancel: cancel,
			}
			nl := &NodeLifecycle{
				events: make(chan NodeEvent),
			}
			return nl, nodeRt, factory, nil
		},
	})
	defer ss.Close()

	// No API client configured — if autoApproveDevices were true, this would panic.
	// With autoApproveDevices=false, the approval code is never reached.
	sl, err := ss.Acquire("svc:test", 443, true, false)
	require.NoError(t, err)
	require.NotNil(t, sl)
}

func TestAcquireWithAutoApprove_Success_ReadvertisesListener(t *testing.T) {
	t.Parallel()

	approvalSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer approvalSrv.Close()

	client := testTailscaleClient(approvalSrv.URL)

	vip := &mockVIPAPI{}
	factory := defaultFactory()

	ss := NewServicesServer(ServicesServerConfig{
		Hostname:                 "test",
		Log:                      zerolog.Nop(),
		VIPServiceAPI:            vip,
		APIClient:                client,
		AutoApproveDevices:       true,
		ApprovalReadvertiseDelay: 1 * time.Millisecond,
		LifecycleConfig:          &NodeLifecycleConfig{},
		LifecycleProvider: func(_ context.Context, _ zerolog.Logger, _ NodeLifecycleConfig) (
			*NodeLifecycle, *NodeRuntime, serviceListenerFactory, error,
		) {
			ctx, cancel := context.WithCancel(context.Background())
			nodeRt := &NodeRuntime{
				Ctx:    ctx,
				Cancel: cancel,
				Server: &tsnet.Server{},
			}
			nl := &NodeLifecycle{
				events: make(chan NodeEvent),
			}
			return nl, nodeRt, factory, nil
		},
	})
	defer ss.Close()

	ss.getNodeStatusFunc = func(_ context.Context) (*ipnstate.Status, error) {
		return &ipnstate.Status{
			Self: &ipnstate.PeerStatus{ID: tailcfg.StableNodeID("99")},
		}, nil
	}

	sl, err := ss.Acquire("svc:test", 443, true, false)
	require.NoError(t, err)
	require.NotNil(t, sl)

	assert.Len(t, factory.calls, 2, "ListenService should be called twice (initial + re-advertise)")
	assert.Equal(t, 1, factory.closeCnt, "Close should be called once between the two ListenService calls")
}
