// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package proxymanager

import (
	"errors"
	"fmt"
	"net"

	"github.com/almeidapaulopt/tsdproxy/internal/model"
	"github.com/almeidapaulopt/tsdproxy/internal/proxyproviders"
)

func (proxy *Proxy) initPorts() {
	for k, v := range proxy.Config.Ports {
		log := proxy.log.With().Str("port", k).Logger()

		var ph portHandler
		if v.IsRedirect {
			ph = newPortRedirect(proxy.ctx, v, log)
		} else if v.ProxyProtocol == model.ProtoHTTP || v.ProxyProtocol == model.ProtoHTTPS {
			ph = newPortProxy(
				proxy.ctx, v, log,
				proxy.Config.ProxyAccessLog,
				proxy.ProviderUserMiddleware,
				proxy.metrics,
				proxy.Config.Hostname,
				k, proxy.logBuffer,
				proxy.Config.IdentityHeaders,
				proxy.tracerProvider,
				proxy.httpPort,
				proxy.Config.RateLimitEnabled,
				proxy.Config.RateLimitRPS,
				proxy.Config.RateLimitBurst,
				proxy.proxyAuthToken,
			)
		} else if v.ProxyProtocol == model.ProtoUDP {
			ph = newPortUDP(proxy.ctx, v, log)
		} else {
			ph = newPortTCP(proxy.ctx, v, log)
		}

		proxy.log.Debug().Any("port", ph).Msg("newport")

		proxy.mtx.Lock()
		proxy.ports[k] = ph
		proxy.mtx.Unlock()
	}
}

func (proxy *Proxy) startProvider() error {
	proxy.log.Info().Msg("starting proxy")

	proxy.mtx.RLock()
	portsCount := len(proxy.ports)
	proxy.mtx.RUnlock()

	if portsCount == 0 {
		return errors.New("no ports configured")
	}

	if err := proxy.providerProxy.Start(proxy.ctx); err != nil {
		return fmt.Errorf("error starting with proxy provider: %w", err)
	}

	return nil
}

func (proxy *Proxy) startListeners() error {
	proxy.mtx.RLock()
	portsConfig := proxy.Config.Ports
	proxy.mtx.RUnlock()

	var listenerErrors int
	for k, pc := range portsConfig {
		proxy.log.Debug().Str("port", k).Msg("Starting proxy port")

		if pc.ProxyProtocol == model.ProtoUDP {
			packetConn, err := proxy.providerProxy.GetPacketConn(k)
			if err != nil {
				proxy.log.Error().Err(err).Str("port", k).Msg("Error getting UDP packet conn")
				listenerErrors++
				continue
			}
			proxy.startPacketPort(k, packetConn)
		} else {
			l, err := proxy.getListenerForPort(k, pc)
			if err != nil {
				proxy.log.Error().Err(err).Str("port", k).Msg("Error adding listener")
				listenerErrors++
				continue
			}
			proxy.startPort(k, l)
		}
	}

	if listenerErrors > 0 && listenerErrors == len(portsConfig) {
		return fmt.Errorf("all %d listeners failed", listenerErrors)
	}

	if listenerErrors > 0 {
		proxy.log.Warn().Int("failed", listenerErrors).Int("total", len(portsConfig)).Msg("proxy started with some listener errors")
	}

	return nil
}

func (proxy *Proxy) startPort(name string, l net.Listener) {
	proxy.mtx.RLock()
	defer proxy.mtx.RUnlock()

	if p, ok := proxy.ports[name]; ok {
		go func() {
			if err := p.startWithListener(l); err != nil {
				proxy.log.Error().Err(err).Msg("error starting port")
				proxy.setStatus(model.ProxyStatusError)
			}
		}()
	}
}

func (proxy *Proxy) getListenerForPort(portName string, pc model.PortConfig) (net.Listener, error) {
	needsCustomTLS := proxy.Config.Domain != "" &&
		pc.ProxyProtocol == model.ProtoHTTPS &&
		proxy.tlsProvider != nil &&
		proxy.tlsProvider.Name() != model.TLSProviderTailscale

	if needsCustomTLS {
		return proxy.getCustomTLSListener(portName)
	}

	if pc.ProxyProtocol == model.ProtoTCP {
		if raw, ok := proxy.providerProxy.(proxyproviders.RawTCPListener); ok {
			return raw.GetRawTCPListener(portName)
		}
	}

	return proxy.providerProxy.GetListener(portName)
}

func (proxy *Proxy) startPacketPort(name string, pc net.PacketConn) {
	proxy.mtx.RLock()
	defer proxy.mtx.RUnlock()

	p, ok := proxy.ports[name]
	if !ok {
		pc.Close()
		return
	}

	udp, ok := p.(*udpPort)
	if !ok {
		pc.Close()
		return
	}

	go func() {
		if err := udp.startWithPacketConn(pc); err != nil {
			proxy.log.Error().Err(err).Msg("error starting UDP port")
			proxy.setStatus(model.ProxyStatusError)
		}
	}()
}
