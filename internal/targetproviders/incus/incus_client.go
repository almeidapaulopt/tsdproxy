// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package incus

import (
	"fmt"
	"os"

	incusclient "github.com/lxc/incus/v7/client"
	incusapi "github.com/lxc/incus/v7/shared/api"

	"github.com/almeidapaulopt/tsdproxy/internal/config"
)

type (
	// APIClient abstracts the Incus SDK methods used by the incus target
	// provider. Satisfied by incusclient.InstanceServer, which enables unit
	// testing without an Incus daemon.
	APIClient interface {
		GetInstances(instanceType incusapi.InstanceType) ([]incusapi.Instance, error)
		GetInstance(name string) (*incusapi.Instance, string, error)
		GetInstanceState(name string) (*incusapi.InstanceState, string, error)
		GetEventsByType(eventTypes []string) (EventListener, error)
		Disconnect()
	}

	// EventListener abstracts the Incus SDK event listener lifecycle used by
	// the provider. Satisfied by *incusclient.EventListener. AddChannel is
	// preferred over AddHandler: it delivers events in order through a single
	// channel, while handler callbacks run in unordered goroutines.
	EventListener interface {
		AddChannel(types []string, size int) <-chan incusapi.Event
		Disconnect()
		Wait() error
		IsActive() bool
	}
)

var _ EventListener = (*incusclient.EventListener)(nil)

type (
	// serverClient adapts incusclient.InstanceServer to the APIClient
	// interface. The SDK's GetEventsByType returns the concrete
	// *incusclient.EventListener, which does not match the interface method
	// signature directly, so the methods are re-declared here.
	serverClient struct {
		server incusclient.InstanceServer
	}
)

var _ APIClient = (*serverClient)(nil)

func (s *serverClient) GetInstances(instanceType incusapi.InstanceType) ([]incusapi.Instance, error) {
	return s.server.GetInstances(instanceType)
}

func (s *serverClient) GetInstance(name string) (*incusapi.Instance, string, error) {
	return s.server.GetInstance(name)
}

func (s *serverClient) GetInstanceState(name string) (*incusapi.InstanceState, string, error) {
	return s.server.GetInstanceState(name)
}

func (s *serverClient) GetEventsByType(eventTypes []string) (EventListener, error) {
	listener, err := s.server.GetEventsByType(eventTypes)
	if err != nil {
		return nil, err
	}
	return listener, nil
}

// Disconnect gets rid of the SDK's background goroutines and connections.
func (s *serverClient) Disconnect() {
	s.server.Disconnect()
}

// connect returns an Incus client for the given provider configuration,
// scoped to the configured project. Connection is local unix socket (Socket,
// auto-detected when empty) or remote HTTPS (URL).
func connect(provider *config.IncusTargetProviderConfig) (APIClient, error) {
	if provider.Socket != "" && provider.URL != "" {
		return nil, ErrSocketAndURL
	}

	server, err := connectTransport(provider)
	if err != nil {
		return nil, err
	}

	if provider.Project != "" {
		server = server.UseProject(provider.Project)
	}

	return &serverClient{server: server}, nil
}

func connectTransport(provider *config.IncusTargetProviderConfig) (incusclient.InstanceServer, error) {
	if provider.URL != "" {
		args := &incusclient.ConnectionArgs{
			InsecureSkipVerify: provider.TLSInsecureSkipVerify,
			UserAgent:          "tsdproxy",
		}

		// Pin the server certificate by identity instead of by hostname,
		// for remotes reached by IP address (no matching SAN in the
		// self-signed server certificate).
		if provider.TLSIdenticalCertificate {
			args.IdenticalCertificate = true
		}

		if err := loadTLSMaterial(provider, args); err != nil {
			return nil, err
		}

		return incusclient.ConnectIncus(provider.URL, args)
	}

	// Empty socket lets the SDK auto-detect the local Incus unix socket.
	return incusclient.ConnectIncusUnix(provider.Socket, nil)
}

// loadTLSMaterial reads the optional client certificate/key pair and server
// CA certificate for remote HTTPS connections.
func loadTLSMaterial(provider *config.IncusTargetProviderConfig, args *incusclient.ConnectionArgs) error {
	if (provider.TLSClientCertFile == "") != (provider.TLSClientKeyFile == "") {
		return ErrTLSClientMaterial
	}

	if provider.TLSClientCertFile != "" {
		cert, err := os.ReadFile(provider.TLSClientCertFile) //nolint:gosec // G304: path validated by config validator
		if err != nil {
			return fmt.Errorf("error reading tls client certificate: %w", err)
		}

		key, err := os.ReadFile(provider.TLSClientKeyFile) //nolint:gosec // G304: path validated by config validator
		if err != nil {
			return fmt.Errorf("error reading tls client key: %w", err)
		}

		args.TLSClientCert = string(cert)
		args.TLSClientKey = string(key)
	}

	if provider.TLSServerCertFile != "" {
		ca, err := os.ReadFile(provider.TLSServerCertFile) //nolint:gosec // G304: path validated by config validator
		if err != nil {
			return fmt.Errorf("error reading tls server certificate: %w", err)
		}

		args.TLSServerCert = string(ca)
	}

	return nil
}
