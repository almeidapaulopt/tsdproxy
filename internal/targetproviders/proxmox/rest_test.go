// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package proxmox

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/almeidapaulopt/tsdproxy/internal/config"
	"github.com/almeidapaulopt/tsdproxy/internal/core/secretstring"
)

// newPVE returns a test server exposing a minimal Proxmox VE API plus a
// factory for provider configs pointing at it, and the Authorization
// header capture. The /version endpoint is always served (newRestClient's
// connectivity check); everything else goes to handler.
func newPVE(
	t *testing.T, handler http.HandlerFunc,
) (func(mutate ...func(*config.ProxmoxTargetProviderConfig)) *config.ProxmoxTargetProviderConfig, func() string) {
	t.Helper()

	var auth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		auth = req.Header.Get("Authorization")

		if req.URL.Path == "/api2/json/version" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":{"version":"9.0.3"}}`))
			return
		}
		handler(w, req)
	}))
	t.Cleanup(server.Close)

	return func(mutate ...func(*config.ProxmoxTargetProviderConfig)) *config.ProxmoxTargetProviderConfig {
		provider := &config.ProxmoxTargetProviderConfig{
			URL:      server.URL,
			APIToken: secretstring.SecretString("root@pam!test=secret-uuid"),
		}
		for _, m := range mutate {
			m(provider)
		}
		return provider
	}, func() string { return auth }
}

func TestRestClient_TokenFormatValidated(t *testing.T) {
	t.Parallel()

	provider := &config.ProxmoxTargetProviderConfig{
		URL:      "https://pve.example.com:8006",
		APIToken: secretstring.SecretString("no-separator"),
	}

	if _, err := newRestClient(provider); err == nil {
		t.Fatal("expected error for token without = separator")
	}
}

func TestRestClient_GetVersion(t *testing.T) {
	t.Parallel()

	newProvider, getAuth := newPVE(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"version":"9.0.3"}}`))
	})

	client, err := newRestClient(newProvider())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer client.Close()

	version, err := client.GetVersion(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if version != "9.0.3" {
		t.Errorf("version: got %q", version)
	}
	if got := getAuth(); got != "PVEAPIToken=root@pam!test=secret-uuid" {
		t.Errorf("auth header: got %q", got)
	}
}

func TestRestClient_ConnectionCheckFails(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"errors":{"token":"permission denied"},"data":null}`))
	}))
	t.Cleanup(server.Close)

	provider := &config.ProxmoxTargetProviderConfig{
		URL:      server.URL,
		APIToken: secretstring.SecretString("root@pam!test=secret-uuid"),
	}

	if _, err := newRestClient(provider); err == nil {
		t.Fatal("expected connection error on 401")
	}
}

func TestRestClient_ListGuests_NodeFilter(t *testing.T) {
	t.Parallel()

	newProvider, _ := newPVE(t, func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path != "/api2/json/cluster/resources" || req.URL.Query().Get("type") != "vm" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[
			{"id":"qemu/100","node":"pve1","vmid":100,"name":"a","type":"qemu","status":"running","template":0},
			{"id":"lxc/101","node":"pve2","vmid":101,"name":"b","type":"lxc","status":"stopped","template":0},
			{"id":"qemu/102","node":"pve1","vmid":102,"name":"c","type":"qemu","status":"stopped","template":1}
		]}`))
	})

	t.Run("NoFilter", func(t *testing.T) {
		t.Parallel()

		client, err := newRestClient(newProvider())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer client.Close()

		guests, err := client.ListGuests(context.Background())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(guests) != 3 {
			t.Errorf("expected 3 guests, got %d", len(guests))
		}
	})

	t.Run("NodeFilter", func(t *testing.T) {
		t.Parallel()

		client, err := newRestClient(newProvider(func(p *config.ProxmoxTargetProviderConfig) { p.Node = "pve2" }))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer client.Close()

		guests, err := client.ListGuests(context.Background())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(guests) != 1 || guests[0].ID != "lxc/101" {
			t.Errorf("expected only lxc/101, got %+v", guests)
		}
	})
}

func TestRestClient_GetGuestConfig(t *testing.T) {
	t.Parallel()

	newProvider, _ := newPVE(t, func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path != "/api2/json/nodes/pve1/qemu/100/config" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"name":"app","notes":"tsdproxy:\n  enable: true\n","tags":"web"}}`))
	})

	client, err := newRestClient(newProvider())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer client.Close()

	cfg, err := client.GetGuestConfig(context.Background(), "pve1", "qemu", "100")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Notes == "" || cfg.Tags != "web" {
		t.Errorf("config: got %+v", cfg)
	}
}

func TestRestClient_QemuAgentInterfaces(t *testing.T) {
	t.Parallel()

	newProvider, _ := newPVE(t, func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path != "/api2/json/nodes/pve1/qemu/100/agent/network-get-interfaces" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"result":[
			{"name":"lo","ip-addresses":[{"ip-address":"127.0.0.1","ip-address-type":"ipv4"}]},
			{"name":"eth0","ip-addresses":[
				{"ip-address":"192.168.1.50","ip-address-type":"ipv4","prefix":24},
				{"ip-address":"fd42::50","ip-address-type":"ipv6"}
			]}
		]}}`))
	})

	client, err := newRestClient(newProvider())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer client.Close()

	interfaces, err := client.GetGuestInterfaces(context.Background(), "pve1", "qemu", "100")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(interfaces) != 2 {
		t.Fatalf("expected 2 interfaces, got %d", len(interfaces))
	}
	if got := interfaces[1].Addresses[0].String(); got != "192.168.1.50" {
		t.Errorf("address: got %q", got)
	}
}

func TestRestClient_LxcInterfaces(t *testing.T) {
	t.Parallel()

	newProvider, _ := newPVE(t, func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path != "/api2/json/nodes/pve1/lxc/101/interfaces" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[
			{"name":"eth0","inet":"10.0.0.5/24","inet6":"fd42::5/64"},
			{"name":"lo","inet":"127.0.0.1/8"}
		]}`))
	})

	client, err := newRestClient(newProvider())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer client.Close()

	interfaces, err := client.GetGuestInterfaces(context.Background(), "pve1", "lxc", "101")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(interfaces[0].Addresses) != 2 {
		t.Fatalf("expected 2 addresses on eth0, got %v", interfaces[0].Addresses)
	}
	if got := interfaces[0].Addresses[0].String(); got != "10.0.0.5" {
		t.Errorf("address: got %q", got)
	}
}

func TestRestClient_ErrorResponse(t *testing.T) {
	t.Parallel()

	newProvider, _ := newPVE(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"errors":{"agent":"QEMU guest agent is not running"},"data":null}`))
	})

	client, err := newRestClient(newProvider())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer client.Close()

	if _, err := client.GetGuestInterfaces(context.Background(), "pve1", "qemu", "100"); err == nil {
		t.Fatal("expected error")
	}
}

func TestRestClient_NullData(t *testing.T) {
	t.Parallel()

	newProvider, _ := newPVE(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":null}`))
	})

	client, err := newRestClient(newProvider())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer client.Close()

	guests, err := client.ListGuests(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(guests) != 0 {
		t.Errorf("expected no guests, got %v", guests)
	}
}
