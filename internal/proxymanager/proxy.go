// SPDX-FileCopyrightText: 2025 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package proxymanager

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"sync"

	"github.com/almeidapaulopt/tsdproxy/internal/model"
	"github.com/almeidapaulopt/tsdproxy/internal/proxyproviders"
	"github.com/rs/zerolog"
)

type (
	// Proxy struct is a struct that contains all the information needed to run a proxy.
	Proxy struct {
		onUpdate func(event model.ProxyEvent)

		log           zerolog.Logger
		ctx           context.Context
		providerProxy proxyproviders.ProxyInterface
		Config        *model.Config
		URL           *url.URL
		cancel        context.CancelFunc
		ports         map[string]*port
		mtx           sync.RWMutex
		status        model.ProxyStatus
	}
)

// NewProxy function is a function that creates a new proxy.
func NewProxy(log zerolog.Logger,
	pcfg *model.Config,
	proxyProvider proxyproviders.Provider,
) (*Proxy, error) {
	//
	var err error

	log = log.With().Str("proxyname", pcfg.Hostname).Logger()
	log.Info().Str("hostname", pcfg.Hostname).Msg("setting up proxy")

	log.Debug().Str("hostname", pcfg.Hostname).
		Msg("initializing proxy")

	// Create the proxyProvider proxy
	//
	pProvider, err := proxyProvider.NewProxy(pcfg)
	if err != nil {
		return nil, fmt.Errorf("error initializing proxy on proxyProvider: %w", err)
	}

	log.Debug().
		Str("hostname", pcfg.Hostname).
		Msg("Proxy server created successfully")

	ctx, cancel := context.WithCancel(context.Background())

	p := &Proxy{
		log:           log,
		Config:        pcfg,
		ctx:           ctx,
		cancel:        cancel,
		providerProxy: pProvider,
		ports:         make(map[string]*port),
	}

	p.initPorts()

	return p, err
}

func (proxy *Proxy) Start() {
	go func() {
		go proxy.start()
		for event := range proxy.providerProxy.WatchEvents() {
			proxy.setStatus(event.Status)
		}
	}()
}

// Close method is a method that initiate proxy close procedure.
func (proxy *Proxy) Close() {
	proxy.setStatus(model.ProxyStatusStopping)

	// cancel context
	proxy.cancel()

	// make sure all listeners are closed
	proxy.close()

	proxy.setStatus(model.ProxyStatusStopped)
}

func (proxy *Proxy) GetStatus() model.ProxyStatus {
	proxy.mtx.RLock()
	defer proxy.mtx.RUnlock()

	return proxy.status
}

func (proxy *Proxy) GetURL() string {
	return proxy.providerProxy.GetURL()
}

func (proxy *Proxy) GetAuthURL() string {
	return proxy.providerProxy.GetAuthURL()
}

func (proxy *Proxy) ProviderUserMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		who := proxy.providerProxy.Whois(r)

		ctx := model.WhoisNewContext(r.Context(), who)

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (proxy *Proxy) initPorts() {
	var newPort *port
	var err error
	for k, v := range proxy.Config.Ports {
		log := proxy.log.With().Str("port", k).Logger()
		switch {
		case v.ProxyProtocol == "tcp":
			newPort, err = newPortTCP(proxy.ctx, v, log)
			if err != nil {
				log.Error().Err(err).Msg("error creating TCP port")
				continue
			}

		case v.IsRedirect:
			newPort = newPortRedirect(proxy.ctx, v, log)

		default:
			newPort = newPortProxy(proxy.ctx, v, log, proxy.Config.ProxyAccessLog, proxy.ProviderUserMiddleware)
		}

		proxy.log.Debug().Any("port", newPort).Msg("newport")

		proxy.mtx.Lock()
		proxy.ports[k] = newPort
		proxy.mtx.Unlock()
	}
}

// Start method is a method that starts the proxy.
func (proxy *Proxy) start() {
	proxy.log.Info().Msg("starting proxy")

	proxy.mtx.RLock()
	portsConfig := proxy.Config.Ports
	portsCount := len(proxy.ports)
	proxy.mtx.RUnlock()

	if portsCount == 0 {
		proxy.log.Warn().Msg("No ports configured")
		proxy.setStatus(model.ProxyStatusError)

		return
	}

	if err := proxy.providerProxy.Start(proxy.ctx); err != nil {
		proxy.log.Error().Err(err).Msg("Error starting with proxy provider")
		proxy.Close()
		return
	}

	var l net.Listener
	var err error

	for k := range portsConfig {
		proxy.log.Debug().Str("port", k).Msg("Starting proxy port")

		l, err = proxy.providerProxy.GetListener(k)
		if err != nil {
			proxy.log.Error().Err(err).Str("port", k).Msg("Error adding listener")
			continue
		}

		proxy.startPort(k, l)
	}
}

func (proxy *Proxy) startPort(name string, l net.Listener) {
	proxy.mtx.RLock()
	defer proxy.mtx.RUnlock()

	// make sure port exists
	if p, ok := proxy.ports[name]; ok {
		go func() {
			if err := p.startWithListener(l); err != nil {
				proxy.log.Error().Err(err).Msg("error starting port")
				proxy.setStatus(model.ProxyStatusError)
			}
		}()
	}
}

// close method is a method that closes all listeners ans httpServer.
func (proxy *Proxy) close() {
	var errs error
	proxy.log.Info().Str("name", proxy.Config.Hostname).Msg("stopping proxy")

	for _, p := range proxy.ports {
		errs = errors.Join(errs, p.close())
	}
	if proxy.providerProxy != nil {
		errs = errors.Join(proxy.providerProxy.Close())
	}

	if errs != nil {
		proxy.log.Error().Err(errs).Msg("Error stopping proxy")
	}

	proxy.log.Info().Str("name", proxy.Config.Hostname).Msg("proxy stopped")
}

func (proxy *Proxy) setStatus(status model.ProxyStatus) {
	proxy.mtx.Lock()

	if proxy.status == status {
		proxy.mtx.Unlock()
		return
	}

	proxy.status = status
	proxy.mtx.Unlock()

	if proxy.onUpdate != nil {
		proxy.onUpdate(model.ProxyEvent{
			ID:     proxy.Config.Hostname,
			Status: status,
		})
	}
}
