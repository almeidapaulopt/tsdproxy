// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package proxmox

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/almeidapaulopt/tsdproxy/internal/config"
	"github.com/almeidapaulopt/tsdproxy/internal/core/httpclient"
)

type (
	// GuestResource is a guest entry of the cluster resources endpoint.
	// ID is "<type>/<vmid>" (e.g. "qemu/100", "lxc/101") and is used as the
	// target ID across the provider.
	GuestResource struct {
		ID       string `json:"id"`
		Node     string `json:"node"`
		Name     string `json:"name"`
		Type     string `json:"type"`
		Status   string `json:"status"`
		VMID     int    `json:"vmid"`
		Template int    `json:"template"`
	}

	// GuestConfig is the subset of a guest config endpoint relevant to the
	// provider. Notes holds the qemu "notes" or LXC "description" field,
	// wherever the tsdproxy YAML block lives.
	GuestConfig struct {
		Name        string `json:"name"`
		Notes       string `json:"notes"`
		Description string `json:"description"`
		Tags        string `json:"tags"`
	}

	// GuestInterface is a normalized guest network interface.
	GuestInterface struct {
		Name      string
		Addresses []netip.Addr
	}

	// APIClient abstracts the Proxmox VE API calls used by the provider.
	// There is no official Go SDK, so the hand-rolled restClient satisfies
	// it, and unit tests use a mock implementation.
	APIClient interface {
		GetVersion(ctx context.Context) (string, error)
		ListGuests(ctx context.Context) ([]GuestResource, error)
		GetGuestConfig(ctx context.Context, node, guestType, vmid string) (*GuestConfig, error)
		GetGuestInterfaces(ctx context.Context, node, guestType, vmid string) ([]GuestInterface, error)
		Close()
	}
)

const (
	apiPathPrefix  = "/api2/json"
	authTokenIntro = "PVEAPIToken=" //nolint:gosec // G101: header scheme prefix, not a credential

	// cidrSplitParts bounds the split of "addr/prefix" LXC interface
	// entries.
	cidrSplitParts = 2

	// lxcMaxAddressesPerInterface bounds the address slice per LXC
	// interface (one IPv4 + one IPv6 entry).
	lxcMaxAddressesPerInterface = 2
	requestTimeout              = 30 * time.Second

	// maxResponseBytes caps response body reads. Config and resource
	// payloads are a few KB; notes-heavy configs stay far below this.
	maxResponseBytes = 4 << 20
)

var _ APIClient = (*restClient)(nil)

type (
	// restClient is a minimal Proxmox VE REST client using API token
	// authentication.
	restClient struct {
		doer    httpclient.Doer
		baseURL string
		token   string
		node    string
	}
)

// newRestClient builds a Proxmox REST client from the provider config. An
// optional Doer overrides the default HTTP client (tests, instrumentation).
// Connecting verifies credentials via the version endpoint, so construction
// fails fast on unreachable hosts or invalid tokens.
func newRestClient(provider *config.ProxmoxTargetProviderConfig, doers ...httpclient.Doer) (*restClient, error) {
	token := provider.APIToken.Value()
	if !strings.Contains(token, "=") {
		return nil, ErrInvalidAPIToken
	}

	doer := httpclient.Doer(&http.Client{Timeout: requestTimeout})
	if len(doers) > 0 && doers[0] != nil {
		doer = doers[0]
	} else if provider.TLSInsecureSkipVerify || provider.TLSCACertFile != "" {
		transport, err := newTLSTransport(provider.TLSInsecureSkipVerify, provider.TLSCACertFile)
		if err != nil {
			return nil, err
		}
		doer = &http.Client{Transport: transport, Timeout: requestTimeout}
	}

	client := &restClient{
		baseURL: strings.TrimRight(provider.URL, "/"),
		token:   token,
		node:    provider.Node,
		doer:    doer,
	}

	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()

	if _, err := client.GetVersion(ctx); err != nil {
		return nil, fmt.Errorf("error connecting to Proxmox VE at %s: %w", provider.URL, err)
	}

	return client, nil
}

// newTLSTransport returns an HTTP transport with the provider TLS options:
// a custom CA certificate file and/or certificate verification skip.
func newTLSTransport(insecureSkipVerify bool, caCertFile string) (http.RoundTripper, error) {
	transport := http.DefaultTransport.(*http.Transport).Clone()

	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12} //nolint:gosec // G402: InsecureSkipVerify below is an explicit operator opt-in
	if insecureSkipVerify {
		tlsConfig.InsecureSkipVerify = true //nolint:gosec // operator opt-in via tlsInsecureSkipVerify
	}

	if caCertFile != "" {
		ca, err := os.ReadFile(caCertFile) //nolint:gosec // G304: path validated by config validator
		if err != nil {
			return nil, fmt.Errorf("error reading tlsCaCertFile: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(ca) {
			return nil, fmt.Errorf("no valid certificate found in %s", caCertFile)
		}
		tlsConfig.RootCAs = pool
	}

	transport.TLSClientConfig = tlsConfig

	return transport, nil
}

// GetVersion returns the PVE version string, used as a connectivity and
// credentials check.
func (r *restClient) GetVersion(ctx context.Context) (string, error) {
	var data struct {
		Version string `json:"version"`
	}
	if err := r.get(ctx, "/version", &data); err != nil {
		return "", err
	}
	return data.Version, nil
}

// ListGuests returns the cluster guests (qemu + lxc). When a node filter is
// configured, guests on other nodes are dropped.
func (r *restClient) ListGuests(ctx context.Context) ([]GuestResource, error) {
	var guests []GuestResource
	if err := r.get(ctx, "/cluster/resources?type=vm", &guests); err != nil {
		return nil, err
	}

	filtered := make([]GuestResource, 0, len(guests))
	for _, guest := range guests {
		if r.node != "" && guest.Node != r.node {
			continue
		}
		filtered = append(filtered, guest)
	}

	return filtered, nil
}

// GetGuestConfig returns the config of a guest. guestType is "qemu" or "lxc".
func (r *restClient) GetGuestConfig(ctx context.Context, node, guestType, vmid string) (*GuestConfig, error) {
	var cfg GuestConfig
	if err := r.get(ctx, guestConfigPath(node, guestType, vmid), &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// GetGuestInterfaces returns the network interfaces of a running guest.
// qemu guests need the guest agent (network-get-interfaces); LXC guests use
// the interfaces endpoint.
func (r *restClient) GetGuestInterfaces(ctx context.Context, node, guestType, vmid string) ([]GuestInterface, error) {
	switch guestType {
	case guestTypeQemu:
		return r.getQemuAgentInterfaces(ctx, node, vmid)
	case guestTypeLXC:
		return r.getLxcInterfaces(ctx, node, vmid)
	default:
		return nil, fmt.Errorf("%w: %s", ErrInvalidGuestID, guestType+"/"+vmid)
	}
}

func (r *restClient) getQemuAgentInterfaces(ctx context.Context, node, vmid string) ([]GuestInterface, error) {
	var data struct {
		Result []struct {
			Name        string `json:"name"`
			IPAddresses []struct {
				Address string `json:"ip-address"`      //nolint:tagliatelle // Proxmox VE wire format
				Type    string `json:"ip-address-type"` //nolint:tagliatelle // Proxmox VE wire format
			} `json:"ip-addresses"` //nolint:tagliatelle // Proxmox VE wire format
		} `json:"result"`
	}

	if err := r.get(ctx, "/nodes/"+url.PathEscape(node)+"/qemu/"+url.PathEscape(vmid)+"/agent/network-get-interfaces", &data); err != nil {
		return nil, err
	}

	interfaces := make([]GuestInterface, 0, len(data.Result))
	for _, iface := range data.Result {
		addresses := make([]netip.Addr, 0, len(iface.IPAddresses))
		for _, ip := range iface.IPAddresses {
			if addr, err := netip.ParseAddr(ip.Address); err == nil {
				addresses = append(addresses, addr)
			}
		}
		interfaces = append(interfaces, GuestInterface{Name: iface.Name, Addresses: addresses})
	}

	return interfaces, nil
}

func (r *restClient) getLxcInterfaces(ctx context.Context, node, vmid string) ([]GuestInterface, error) {
	var data []struct {
		Name  string `json:"name"`
		Inet  string `json:"inet"`
		Inet6 string `json:"inet6"`
	}

	if err := r.get(ctx, "/nodes/"+url.PathEscape(node)+"/lxc/"+url.PathEscape(vmid)+"/interfaces", &data); err != nil {
		return nil, err
	}

	interfaces := make([]GuestInterface, 0, len(data))
	for _, iface := range data {
		// entries are "addr/prefix"; the prefix is irrelevant here.
		rawAddresses := make([]string, 0, lxcMaxAddressesPerInterface)
		if iface.Inet != "" {
			rawAddresses = append(rawAddresses, iface.Inet)
		}
		if iface.Inet6 != "" {
			rawAddresses = append(rawAddresses, iface.Inet6)
		}

		addresses := make([]netip.Addr, 0, len(rawAddresses))
		for _, raw := range rawAddresses {
			raw = strings.SplitN(raw, "/", cidrSplitParts)[0]
			if addr, err := netip.ParseAddr(raw); err == nil {
				addresses = append(addresses, addr)
			}
		}
		interfaces = append(interfaces, GuestInterface{Name: iface.Name, Addresses: addresses})
	}

	return interfaces, nil
}

// Close releases idle connections of the default HTTP client. Injected
// Doers are owned by the caller and left untouched.
func (r *restClient) Close() {
	if client, ok := r.doer.(*http.Client); ok {
		client.CloseIdleConnections()
	}
}

// get performs a GET request and decodes the "data" member of the PVE JSON
// envelope into out.
func (r *restClient) get(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.baseURL+apiPathPrefix+path, nil)
	if err != nil {
		return fmt.Errorf("error building request: %w", err)
	}

	req.Header.Set("Authorization", authTokenIntro+r.token)

	resp, err := r.doer.Do(req)
	if err != nil {
		return fmt.Errorf("error executing request: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck // read errors on close are irrelevant here

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return fmt.Errorf("error reading response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("proxmox api %s: %s: %s", path, resp.Status, pveErrorMessage(body))
	}

	var envelope struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return fmt.Errorf("error decoding response of %s: %w", path, err)
	}

	// data may be null (e.g. empty result sets) — leave out untouched.
	if len(envelope.Data) == 0 || string(envelope.Data) == "null" {
		return nil
	}

	if err := json.Unmarshal(envelope.Data, out); err != nil {
		return fmt.Errorf("error decoding data of %s: %w", path, err)
	}

	return nil
}

// pveErrorMessage extracts the human-readable error message of a PVE error
// response.
func pveErrorMessage(body []byte) string {
	var errResponse struct {
		Errors map[string]string `json:"errors"`
	}
	if err := json.Unmarshal(body, &errResponse); err != nil || len(errResponse.Errors) == 0 {
		return strings.TrimSpace(string(body))
	}

	parts := make([]string, 0, len(errResponse.Errors))
	for key, value := range errResponse.Errors {
		parts = append(parts, key+": "+value)
	}

	return strings.Join(parts, "; ")
}

func guestConfigPath(node, guestType, vmid string) string {
	return "/nodes/" + url.PathEscape(node) + "/" + url.PathEscape(guestType) + "/" + url.PathEscape(vmid) + "/config"
}
