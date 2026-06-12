// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package tailscale

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"sync"
	"time"

	"github.com/rs/zerolog"
	"golang.org/x/sync/singleflight"
	"tailscale.com/client/local"
	"tailscale.com/tsnet"

	"github.com/almeidapaulopt/tsdproxy/internal/model"
)

// TrafficExposure defines the contract for how a Tailscale node exposes
// traffic to containers. Each mode (per-proxy, shared SNI, services/VIP)
// implements this interface to handle port listeners, routing, and teardown.
type TrafficExposure interface {
	Start(ctx context.Context, runtime *NodeRuntime, cfg *model.Config) error
	Close(ctx context.Context) error
}

// ListenerExposure is an optional interface for exposures that provide
// protocol-level listeners (HTTP, HTTPS, TCP).
type ListenerExposure interface {
	TrafficExposure
	GetListener(portName string) (net.Listener, error)
}

type RawTCPExposure interface {
	TrafficExposure
	GetRawTCPListener(portName string) (net.Listener, error)
}

type PacketExposure interface {
	TrafficExposure
	GetPacketConn(portName string) (net.PacketConn, error)
}

var errExposureNotStarted = errors.New("exposure not started")

// exposureLookup returns the value from m[portName] or an appropriate error
// if the exposure hasn't been started or the port doesn't exist.
func exposureLookup[T any](started bool, m map[string]T, portName string) (T, error) {
	var zero T
	if !started {
		return zero, errExposureNotStarted
	}
	v, ok := m[portName]
	if !ok {
		return zero, fmt.Errorf("%w: %s", ErrProxyPortNotFound, portName)
	}
	return v, nil
}

// PerProxyExposure creates direct port listeners on a per-proxy tsnet.Server.
type PerProxyExposure struct {
	log          zerolog.Logger
	listeners    map[string]net.Listener
	rawListeners map[string]net.Listener
	packetConns  map[string]net.PacketConn
	runtime      *NodeRuntime
	mtx          sync.RWMutex
	started      bool
}

// NewPerProxyExposure creates a new PerProxyExposure instance.
func NewPerProxyExposure(log zerolog.Logger) *PerProxyExposure {
	return &PerProxyExposure{log: log}
}

// Start creates port listeners on the tsnet.Server for each port in the config.
func (e *PerProxyExposure) Start(_ context.Context, runtime *NodeRuntime, cfg *model.Config) error {
	e.mtx.Lock()
	defer e.mtx.Unlock()

	if e.started {
		return nil
	}

	e.runtime = runtime
	e.listeners = make(map[string]net.Listener)
	e.rawListeners = make(map[string]net.Listener)
	e.packetConns = make(map[string]net.PacketConn)

	ts := runtime.Server

	for portName, portCfg := range cfg.Ports {
		switch portCfg.ProxyProtocol {
		case model.ProtoHTTPS:
			l, err := e.createHTTPSListener(ts, portCfg)
			if err != nil {
				e.closeAll()
				return fmt.Errorf("create HTTPS listener for port %q: %w", portName, err)
			}
			e.listeners[portName] = l

		case model.ProtoHTTP:
			l, err := e.createPlainListener(ts, portCfg)
			if err != nil {
				e.closeAll()
				return fmt.Errorf("create HTTP listener for port %q: %w", portName, err)
			}
			e.listeners[portName] = l

		case model.ProtoTCP:
			l, err := e.createPlainListener(ts, portCfg)
			if err != nil {
				e.closeAll()
				return fmt.Errorf("create TCP listener for port %q: %w", portName, err)
			}
			e.rawListeners[portName] = l

		case model.ProtoUDP:
			pc, err := e.createUDPConn(runtime, ts, portCfg)
			if err != nil {
				e.closeAll()
				return fmt.Errorf("create UDP conn for port %q: %w", portName, err)
			}
			e.packetConns[portName] = pc
		}
	}

	e.started = true
	return nil
}

func (e *PerProxyExposure) createHTTPSListener(ts TSNetServer, cfg model.PortConfig) (net.Listener, error) {
	addr := net.JoinHostPort("", strconv.Itoa(cfg.ProxyPort))
	if cfg.Tailscale.Funnel {
		return ts.ListenFunnel("tcp", addr) //nolint:gosec
	}
	return ts.ListenTLS("tcp", addr) //nolint:gosec
}

func (e *PerProxyExposure) createPlainListener(ts TSNetServer, cfg model.PortConfig) (net.Listener, error) {
	addr := net.JoinHostPort("", strconv.Itoa(cfg.ProxyPort))
	return ts.Listen("tcp", addr) //nolint:gosec
}

func (e *PerProxyExposure) createUDPConn(runtime *NodeRuntime, ts TSNetServer, cfg model.PortConfig) (net.PacketConn, error) {
	ip4, err := e.waitForTailscaleIP(runtime.Ctx, ts)
	if err != nil {
		return nil, fmt.Errorf("cannot bind UDP port %d: %w", cfg.ProxyPort, err)
	}
	addr := net.JoinHostPort(ip4.String(), strconv.Itoa(cfg.ProxyPort))
	return ts.ListenPacket("udp", addr)
}

func (e *PerProxyExposure) waitForTailscaleIP(ctx context.Context, ts TSNetServer) (netip.Addr, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	const (
		interval = 500 * time.Millisecond
		timeout  = 30 * time.Second
	)

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		ip4, _ := ts.TailscaleIPs()
		if ip4.IsValid() {
			return ip4, nil
		}
		select {
		case <-ctx.Done():
			return netip.Addr{}, errors.New("timed out waiting for tailscale IP")
		case <-ticker.C:
		}
	}
}

func (e *PerProxyExposure) Close(_ context.Context) error {
	e.mtx.Lock()
	defer e.mtx.Unlock()

	if !e.started {
		return nil
	}

	e.closeAll()
	e.runtime = nil
	e.started = false
	return nil
}

func (e *PerProxyExposure) closeAll() {
	for _, l := range e.listeners {
		if err := l.Close(); err != nil {
			e.log.Warn().Err(err).Msg("failed to close listener")
		}
	}
	for _, l := range e.rawListeners {
		if err := l.Close(); err != nil {
			e.log.Warn().Err(err).Msg("failed to close raw listener")
		}
	}
	for _, pc := range e.packetConns {
		if err := pc.Close(); err != nil {
			e.log.Warn().Err(err).Msg("failed to close packet conn")
		}
	}
	e.listeners = nil
	e.rawListeners = nil
	e.packetConns = nil
}

func (e *PerProxyExposure) GetListener(portName string) (net.Listener, error) {
	e.mtx.RLock()
	defer e.mtx.RUnlock()
	return exposureLookup(e.started, e.listeners, portName)
}

func (e *PerProxyExposure) GetRawTCPListener(portName string) (net.Listener, error) {
	e.mtx.RLock()
	defer e.mtx.RUnlock()
	return exposureLookup(e.started, e.rawListeners, portName)
}

func (e *PerProxyExposure) GetPacketConn(portName string) (net.PacketConn, error) {
	e.mtx.RLock()
	defer e.mtx.RUnlock()
	return exposureLookup(e.started, e.packetConns, portName)
}

type SharedSNIExposure struct {
	certCacheTime   time.Time
	certSF          singleflight.Group
	cfg             *model.Config
	packetConns     map[string]net.PacketConn
	tlsListeners    map[string]net.Listener
	sharedServer    *SharedServer
	directListeners map[string]net.Listener
	cachedCert      *tls.Certificate
	vListeners      map[string]*VirtualListener
	domain          string
	mtx             sync.RWMutex
	certCacheMtx    sync.Mutex
	started         bool
}

const certCacheTTL = 5 * time.Minute

// NewSharedSNIExposure creates a new SharedSNIExposure backed by the given SharedServer.
func NewSharedSNIExposure(sharedServer *SharedServer, domain string) *SharedSNIExposure {
	return &SharedSNIExposure{
		sharedServer: sharedServer,
		domain:       domain,
	}
}

//nolint:dupl // mirrors SharedProxy.Start pattern for port acquisition
func (e *SharedSNIExposure) Start(ctx context.Context, _ *NodeRuntime, cfg *model.Config) error {
	e.mtx.Lock()
	defer e.mtx.Unlock()

	if e.started {
		return nil
	}

	e.cfg = cfg
	e.vListeners = make(map[string]*VirtualListener)
	e.directListeners = make(map[string]net.Listener)
	e.tlsListeners = make(map[string]net.Listener)

	for portName, portCfg := range cfg.Ports {
		select {
		case <-ctx.Done():
			e.rollbackAcquired()
			return ctx.Err()
		default:
		}

		if err := e.acquirePort(portName, portCfg); err != nil {
			e.rollbackAcquired()
			return err
		}
	}

	e.started = true
	return nil
}

func (e *SharedSNIExposure) acquirePort(portName string, portCfg model.PortConfig) error {
	switch portCfg.ProxyProtocol {
	case model.ProtoHTTPS:
		vl, _, err := e.sharedServer.Acquire(e.domain, portCfg.ProxyPort, portCfg.ProxyProtocol)
		if err != nil {
			return err
		}
		e.vListeners[portName] = vl
		e.tlsListeners[portName] = tls.NewListener(vl, &tls.Config{
			MinVersion: tls.VersionTLS12,
			GetCertificate: func(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
				if hello.ServerName != "" && hello.ServerName != e.domain {
					return nil, fmt.Errorf("SNI mismatch: got %q, want %q", hello.ServerName, e.domain)
				}
				lc := e.sharedServer.GetLocalClient()
				if lc == nil {
					return nil, errors.New("shared server local client not available")
				}
				return e.getCertificate(lc)
			},
		})

	case model.ProtoHTTP:
		vl, _, err := e.sharedServer.Acquire(e.domain, portCfg.ProxyPort, portCfg.ProxyProtocol)
		if err != nil {
			return err
		}
		e.vListeners[portName] = vl

	case model.ProtoTCP:
		_, direct, err := e.sharedServer.Acquire(e.domain, portCfg.ProxyPort, portCfg.ProxyProtocol)
		if err != nil {
			return err
		}
		e.directListeners[portName] = direct

	case model.ProtoUDP:
		pc, err := e.sharedServer.AcquirePacket(e.domain, portCfg.ProxyPort)
		if err != nil {
			return err
		}
		if e.packetConns == nil {
			e.packetConns = make(map[string]net.PacketConn)
		}
		e.packetConns[portName] = pc
	}
	return nil
}

func (e *SharedSNIExposure) rollbackAcquired() {
	if e.cfg == nil {
		return
	}
	for portName, portCfg := range e.cfg.Ports {
		if _, ok := e.vListeners[portName]; ok {
			// SharedServer.Release handles all cleanup (VirtualListener
			// closure via router.Unregister, underlying listener close
			// when the router is empty).
			e.sharedServer.Release(e.domain, portCfg.ProxyPort, portCfg.ProxyProtocol)
			delete(e.vListeners, portName)
		}
		if _, ok := e.directListeners[portName]; ok {
			// SharedServer.Release closes the direct listener and
			// removes the port entry from the runtime.
			e.sharedServer.Release(e.domain, portCfg.ProxyPort, portCfg.ProxyProtocol)
			delete(e.directListeners, portName)
		}
		if _, ok := e.packetConns[portName]; ok {
			e.sharedServer.ReleasePacket(e.domain, portCfg.ProxyPort)
			delete(e.packetConns, portName)
		}
	}
	e.vListeners = nil
	e.directListeners = nil
	e.packetConns = nil
	e.cfg = nil
}

func (e *SharedSNIExposure) Close(_ context.Context) error {
	e.mtx.Lock()
	defer e.mtx.Unlock()

	if !e.started {
		return nil
	}
	e.rollbackAcquired()
	e.cfg = nil
	e.tlsListeners = nil
	e.started = false

	e.certCacheMtx.Lock()
	e.cachedCert = nil
	e.certCacheMtx.Unlock()

	return nil
}

// getCertificate returns a cached TLS certificate for this exposure's domain.
// CertPairToTLSCertificate involves IPC to the local Tailscale client and
// crypto parsing — caching avoids repeating this on every TLS handshake, and
// singleflight deduplicates concurrent misses on the same domain.
func (e *SharedSNIExposure) getCertificate(lc *local.Client) (*tls.Certificate, error) {
	e.certCacheMtx.Lock()
	if e.cachedCert != nil && time.Since(e.certCacheTime) < certCacheTTL {
		cert := e.cachedCert
		e.certCacheMtx.Unlock()
		return cert, nil
	}
	e.certCacheMtx.Unlock()

	v, err, _ := e.certSF.Do("cert:"+e.domain, func() (any, error) {
		// Re-check after winning the race — another goroutine may have
		// populated the cache while we waited for the singleflight leader.
		e.certCacheMtx.Lock()
		if e.cachedCert != nil && time.Since(e.certCacheTime) < certCacheTTL {
			cert := e.cachedCert
			e.certCacheMtx.Unlock()
			return cert, nil
		}
		e.certCacheMtx.Unlock()

		cert, err := CertPairToTLSCertificate(context.Background(), lc, e.domain)
		if err != nil {
			return nil, err
		}

		e.certCacheMtx.Lock()
		e.cachedCert = cert
		e.certCacheTime = time.Now()
		e.certCacheMtx.Unlock()

		return cert, nil
	})
	if err != nil {
		return nil, err
	}
	return v.(*tls.Certificate), nil
}

// GetListener returns the listener for the given port configuration.
// For HTTPS ports, the VirtualListener is wrapped with TLS termination.
func (e *SharedSNIExposure) GetListener(portName string) (net.Listener, error) {
	e.mtx.RLock()
	defer e.mtx.RUnlock()

	if !e.started {
		return nil, errExposureNotStarted
	}

	portCfg, ok := e.cfg.Ports[portName]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrProxyPortNotFound, portName)
	}

	switch portCfg.ProxyProtocol {
	case model.ProtoHTTPS:
		return exposureLookup(e.started, e.tlsListeners, portName)

	case model.ProtoHTTP:
		return exposureLookup(e.started, e.vListeners, portName)

	default:
		return nil, fmt.Errorf("%w: %s", ErrProxyPortNotFound, portName)
	}
}

type ServicesVIPExposure struct {
	listeners      map[string]*tsnet.ServiceListener
	servicesServer *ServicesServer
	cfg            *model.Config
	serviceName    string
	mtx            sync.RWMutex
	started        bool
}

func NewServicesVIPExposure(servicesServer *ServicesServer, serviceName string) *ServicesVIPExposure {
	return &ServicesVIPExposure{
		servicesServer: servicesServer,
		serviceName:    serviceName,
	}
}

func (e *ServicesVIPExposure) Start(ctx context.Context, _ *NodeRuntime, cfg *model.Config) error {
	e.mtx.Lock()
	defer e.mtx.Unlock()

	if e.started {
		return nil
	}

	e.cfg = cfg
	e.listeners = make(map[string]*tsnet.ServiceListener)

	for portName, portCfg := range cfg.Ports {
		select {
		case <-ctx.Done():
			e.rollbackAcquired()
			return ctx.Err()
		default:
		}

		var (
			listener *tsnet.ServiceListener
			err      error
		)

		switch portCfg.ProxyProtocol {
		case model.ProtoHTTPS:
			listener, err = e.servicesServer.Acquire(e.serviceName, uint16(portCfg.ProxyPort), true, false) //nolint:gosec
		case model.ProtoHTTP:
			listener, err = e.servicesServer.Acquire(e.serviceName, uint16(portCfg.ProxyPort), false, false) //nolint:gosec
		case model.ProtoTCP:
			listener, err = e.servicesServer.Acquire(e.serviceName, uint16(portCfg.ProxyPort), false, true) //nolint:gosec
		default:
			e.rollbackAcquired()
			return fmt.Errorf("services mode does not support protocol %q", portCfg.ProxyProtocol)
		}

		if err != nil {
			e.rollbackAcquired()
			return err
		}

		e.listeners[portName] = listener
	}

	e.started = true
	return nil
}

func (e *ServicesVIPExposure) rollbackAcquired() {
	if e.cfg == nil {
		return
	}
	for portName, portCfg := range e.cfg.Ports {
		if _, ok := e.listeners[portName]; ok {
			if err := e.servicesServer.Release(e.serviceName, uint16(portCfg.ProxyPort)); err != nil { //nolint:gosec
				e.servicesServer.log.Error().Err(err).Str("port", portName).Msg("failed to release service")
			}
			delete(e.listeners, portName)
		}
	}
	e.listeners = nil
	e.cfg = nil
}

func (e *ServicesVIPExposure) Close(_ context.Context) error {
	e.mtx.Lock()
	defer e.mtx.Unlock()

	if !e.started {
		return nil
	}
	e.rollbackAcquired()
	e.cfg = nil
	e.started = false
	return nil
}

func (e *ServicesVIPExposure) GetListener(portName string) (net.Listener, error) {
	e.mtx.RLock()
	defer e.mtx.RUnlock()
	return exposureLookup(e.started, e.listeners, portName)
}

func (e *ServicesVIPExposure) firstFQDN() string {
	e.mtx.RLock()
	defer e.mtx.RUnlock()
	for _, sl := range e.listeners {
		return sl.FQDN
	}
	return ""
}

func (e *SharedSNIExposure) getRawTCPListener(portName string) (net.Listener, error) {
	e.mtx.RLock()
	defer e.mtx.RUnlock()

	if !e.started {
		return nil, errExposureNotStarted
	}
	if l, ok := e.directListeners[portName]; ok {
		return l, nil
	}
	if vl, ok := e.vListeners[portName]; ok {
		return vl, nil
	}
	return nil, fmt.Errorf("%w: %s", ErrProxyPortNotFound, portName)
}

func (e *SharedSNIExposure) getPacketConn(portName string) (net.PacketConn, error) {
	e.mtx.RLock()
	defer e.mtx.RUnlock()
	return exposureLookup(e.started, e.packetConns, portName)
}

var (
	_ TrafficExposure  = (*PerProxyExposure)(nil)
	_ ListenerExposure = (*PerProxyExposure)(nil)
	_ RawTCPExposure   = (*PerProxyExposure)(nil)
	_ PacketExposure   = (*PerProxyExposure)(nil)

	_ TrafficExposure  = (*SharedSNIExposure)(nil)
	_ ListenerExposure = (*SharedSNIExposure)(nil)

	_ TrafficExposure  = (*ServicesVIPExposure)(nil)
	_ ListenerExposure = (*ServicesVIPExposure)(nil)
)
