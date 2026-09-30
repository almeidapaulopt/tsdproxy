// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package proxymanager

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/time/rate"

	"github.com/almeidapaulopt/tsdproxy/internal/core/metrics"
	"github.com/almeidapaulopt/tsdproxy/internal/model"

	"github.com/rs/zerolog"
)

var errRateLimited = errors.New("UDP packet rate limited")

const (
	udpBufSize        = 64 * 1024
	udpIdleTimeout    = 2 * time.Minute
	udpMaxClients     = 1024
	udpPerSourceRate  = 500
	udpPerSourceBurst = 1000
)

// udpPort forwards UDP packets from the Tailscale PacketConn to the target backend.
type udpPort struct {
	log          zerolog.Logger
	conn         net.PacketConn
	ctx          context.Context
	metrics      *metrics.Metrics
	clientMap    map[string]*clientEntry
	cancel       context.CancelFunc
	portName     string
	proxyName    string
	pconfig      model.PortConfig
	wg           sync.WaitGroup
	clientMapMtx sync.Mutex
	mtx          sync.Mutex
	started      atomic.Bool
}

func newPortUDP(ctx context.Context, pconfig model.PortConfig, log zerolog.Logger, metrics *metrics.Metrics, proxyName, portName string) *udpPort {
	ctxPort, cancel := context.WithCancel(ctx)

	return &udpPort{
		log:       log.With().Str("port", pconfig.String()).Logger(),
		ctx:       ctxPort,
		cancel:    cancel,
		pconfig:   pconfig,
		metrics:   metrics,
		proxyName: proxyName,
		portName:  portName,
	}
}

func (p *udpPort) startWithListener(_ net.Listener) error {
	return errors.New("UDP ports must use startWithPacketConn, not startWithListener")
}

func (p *udpPort) startWithPacketConn(pc net.PacketConn) error {
	if err := portStartLock(p.ctx, &p.mtx, &p.started); err != nil {
		pc.Close()
		return fmt.Errorf("udp port closed before start: %w", err)
	}
	target := p.pconfig.GetFirstTarget()
	if target == nil || target.Host == "" {
		p.started.Store(true)
		p.mtx.Unlock()
		pc.Close()
		return errors.New("no target configured for UDP port")
	}

	p.wg.Add(1)
	p.started.Store(true)
	p.conn = pc
	p.clientMap = make(map[string]*clientEntry)
	p.mtx.Unlock()
	defer p.wg.Done()

	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		<-p.ctx.Done()
		pc.Close()
	}()

	p.relayPackets(pc)
	return nil
}

// clientEntry tracks a per-client backend UDP connection and its last activity.
type clientEntry struct {
	conn      *net.UDPConn
	lastSeen  time.Time
	limiter   *rate.Limiter
	closeOnce sync.Once
}

func (e *clientEntry) close() {
	if e.conn != nil {
		e.closeOnce.Do(func() {
			e.conn.Close()
		})
	}
}

func (p *udpPort) relayPackets(pc net.PacketConn) {
	buf := make([]byte, udpBufSize)

	defer closeAllClients(p.clientMap, &p.clientMapMtx)

	for {
		n, clientAddr, err := pc.ReadFrom(buf)
		if err != nil {
			if errors.Is(err, net.ErrClosed) || p.ctx.Err() != nil {
				return
			}
			p.log.Error().Err(err).Msg("error reading UDP packet")
			return
		}

		backend, err := p.getOrCreateBackendConn(clientAddr, p.clientMap, &p.clientMapMtx, pc)
		if err != nil {
			if errors.Is(err, errRateLimited) {
				continue
			}
			p.log.Error().Err(err).Str("client", clientAddr.String()).Msg("error dialing backend")
			continue
		}

		if backend == nil {
			continue
		}

		if _, err := backend.Write(buf[:n]); err != nil {
			if errors.Is(err, net.ErrClosed) {
				continue
			}
			p.log.Error().Err(err).Msg("error writing to backend")
		}
	}
}

func (p *udpPort) updateClientMetric(count int) {
	if p.metrics != nil {
		p.metrics.SetUDPClientsActive(p.proxyName, p.portName, count)
	}
}

func closeAllClients(clientMap map[string]*clientEntry, mapMtx *sync.Mutex) {
	mapMtx.Lock()
	for _, entry := range clientMap {
		entry.close()
	}
	mapMtx.Unlock()
}

// getOrCreateBackendConn returns an existing or new backend UDP connection for
// the client.
//
// The slow operations (target resolution + UDP dial) happen OUTSIDE the
// per-port clientMap mutex to avoid head-of-line blocking: when target.Host
// is a hostname rather than a raw IP, the DNS lookup can take noticeable time,
// and holding the lock across it would also block concurrent udpPort.close()
// (closeAllClients) and relayBackendToClient's deferred map mutation for other
// clients.
//
// Three-phase pattern:
//
//  1. Fast path (under lock): return existing entry if present.
//  2. Resolve + dial (no lock): slow operations allowed to race with other
//     map users.
//  3. Install (under lock): double-check no concurrent winner, then create
//     entry, run rate-limit accounting, evict if needed, start relay goroutine.
//
// Caller must NOT hold mapMtx. The function internally releases and
// re-acquires the lock.
func (p *udpPort) getOrCreateBackendConn(
	clientAddr net.Addr,
	clientMap map[string]*clientEntry,
	mapMtx *sync.Mutex,
	pc net.PacketConn,
) (*net.UDPConn, error) {
	key := clientAddr.String()

	// Phase 1: fast path — return existing entry under the lock.
	mapMtx.Lock()
	if entry, ok := clientMap[key]; ok && entry.conn != nil {
		entry.lastSeen = time.Now()
		if !entry.limiter.Allow() {
			mapMtx.Unlock()
			p.log.Debug().Str("client", clientAddr.String()).Msg("UDP packet rate limited")
			return nil, errRateLimited
		}
		mapMtx.Unlock()
		return entry.conn, nil
	}
	mapMtx.Unlock()

	// Phase 2: resolve target and dial backend WITHOUT holding the lock.
	// target.Host may require DNS resolution; net.DialUDP may block on the
	// OS resolver. Re-resolving per new client lets target re-resolution
	// take effect without restarting the port.
	target := p.pconfig.GetFirstTarget()
	if target == nil || target.Host == "" {
		return nil, errors.New("no target configured for UDP port")
	}

	backendAddr, err := net.ResolveUDPAddr(model.ProtoUDP, target.Host)
	if err != nil {
		return nil, fmt.Errorf("error resolving backend UDP address: %w", err)
	}

	conn, err := net.DialUDP(model.ProtoUDP, nil, backendAddr)
	if err != nil {
		return nil, err
	}

	// Phase 3: install under the lock with a double-check.
	mapMtx.Lock()
	defer mapMtx.Unlock()

	// If the port is shutting down (closeAllClients ran between Phase 2 and
	// Phase 3, or udpPort.close() was called), do not install a new entry —
	// its relay goroutine would never be observed by a subsequent close and
	// the conn would leak.
	if p.ctx.Err() != nil {
		conn.Close()
		return nil, errors.New("udp port closed during backend dial")
	}

	// Defensive double-check: if another caller installed an entry for the
	// same key while we were dialing, close our loser conn and return the
	// existing one. In production relayPackets is single-threaded per port,
	// so this branch is unreachable today; the check is cheap insurance
	// against future callers and against the close-race window above.
	if existing, ok := clientMap[key]; ok && existing.conn != nil {
		existing.lastSeen = time.Now()
		if !existing.limiter.Allow() {
			p.log.Debug().Str("client", clientAddr.String()).Msg("UDP packet rate limited")
			conn.Close()
			return nil, errRateLimited
		}
		conn.Close()
		return existing.conn, nil
	}

	entry := &clientEntry{
		conn:     conn,
		lastSeen: time.Now(),
		limiter:  rate.NewLimiter(udpPerSourceRate, udpPerSourceBurst),
	}
	clientMap[key] = entry

	if !entry.limiter.Allow() {
		// Defensive: with udpPerSourceBurst=1000 the first Allow() always
		// succeeds, so this branch is currently unreachable. Keep the conn
		// cleanup consistent with the Phase-3 double-check path (line 900)
		// so a future constants tweak can't introduce a socket leak — the
		// relay goroutine that would otherwise close this conn hasn't been
		// started yet.
		p.log.Debug().Str("client", clientAddr.String()).Msg("UDP packet rate limited")
		conn.Close()
		delete(clientMap, key)
		p.updateClientMetric(len(clientMap))
		return nil, errRateLimited
	}

	if len(clientMap) >= udpMaxClients {
		evictOldestClient(clientMap)
	}

	p.updateClientMetric(len(clientMap))

	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		p.relayBackendToClient(entry, pc, clientAddr, mapMtx, clientMap)
	}()

	return conn, nil
}

func evictOldestClient(clientMap map[string]*clientEntry) {
	var oldestKey string
	var oldestTime time.Time
	for k, v := range clientMap {
		if oldestKey == "" || v.lastSeen.Before(oldestTime) {
			oldestKey = k
			oldestTime = v.lastSeen
		}
	}
	if oldest, ok := clientMap[oldestKey]; ok {
		oldest.close()
		delete(clientMap, oldestKey)
	}
}

func (p *udpPort) relayBackendToClient(entry *clientEntry, pc net.PacketConn, clientAddr net.Addr, mapMtx *sync.Mutex, clientMap map[string]*clientEntry) {
	backend := entry.conn
	defer func() {
		entry.close()
		mapMtx.Lock()
		// Delete only if this entry hasn't been replaced (e.g. by eviction + re-creation).
		if current, ok := clientMap[clientAddr.String()]; ok && current == entry {
			delete(clientMap, clientAddr.String())
			p.updateClientMetric(len(clientMap))
		}
		mapMtx.Unlock()
	}()

	buf := make([]byte, udpBufSize)
	for {
		if err := backend.SetReadDeadline(time.Now().Add(udpIdleTimeout)); err != nil {
			return
		}

		n, err := backend.Read(buf)
		if err != nil {
			// Idle timeout — client mapping expires gracefully.
			if errors.Is(err, os.ErrDeadlineExceeded) {
				return
			}
			// Conn closed (shutdown or eviction).
			if errors.Is(err, net.ErrClosed) || p.ctx.Err() != nil {
				return
			}
			p.log.Error().Err(err).Msg("error reading from backend")
			return
		}

		if _, err := pc.WriteTo(buf[:n], clientAddr); err != nil {
			return
		}
	}
}

func (p *udpPort) close() error {
	p.mtx.Lock()
	var errs error
	if p.conn != nil {
		errs = errors.Join(errs, p.conn.Close())
	}
	clientMap := p.clientMap
	clientMapMtx := &p.clientMapMtx
	p.mtx.Unlock()

	if clientMap != nil {
		closeAllClients(clientMap, clientMapMtx)
	}
	p.updateClientMetric(0)

	p.cancel()

	if p.started.Load() {
		p.wg.Wait()
	}

	return errs
}
