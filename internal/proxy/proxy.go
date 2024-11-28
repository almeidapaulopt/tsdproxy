// SPDX-FileCopyrightText: 2024 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT
package proxy

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"

	"github.com/almeidapaulopt/tsdproxy/internal/core"
	"github.com/almeidapaulopt/tsdproxy/internal/proxyconfig"
	"github.com/almeidapaulopt/tsdproxy/internal/proxyproviders"
	"github.com/almeidapaulopt/tsdproxy/internal/targetproviders"

	"github.com/rs/zerolog"
)

type (
	// Proxy struct is a struct that contains all the information needed to run a proxy.
	Proxy struct {
		proxyProvider  proxyproviders.Proxy
		targetprovider targetproviders.TargetProvider

		httpListener net.Listener
		reverseProxy *httputil.ReverseProxy
		Config       *proxyconfig.Config

		URL *url.URL

		log      zerolog.Logger
		listener net.Listener
		handler  http.Handler
	}
)

// NewProxy function is a function that creates a new proxy.
func NewProxy(log zerolog.Logger,
	pcfg *proxyconfig.Config,
	proxyProvider proxyproviders.Provider,
	targetprovider targetproviders.TargetProvider,
) (*Proxy, error) {
	//
	var err error

	proxy := &Proxy{
		log:            log.With().Str("proxyname", pcfg.Hostname).Logger(),
		targetprovider: targetprovider,
		Config:         pcfg,
	}

	log.Info().Str("hostname", pcfg.Hostname).Msg("setting up proxy")

	log.Debug().
		Str("hostname", pcfg.Hostname).
		Str("targetURL", pcfg.TargetURL.String()).
		Str("proxyURL", pcfg.ProxyURL.String()).
		Msg("initializing proxy")

	// Create the reverse proxy
	//
	proxy.reverseProxy = &httputil.ReverseProxy{
		Rewrite: func(r *httputil.ProxyRequest) {
			r.SetURL(pcfg.TargetURL)
			r.Out.Host = r.In.Host

			r.Out.Header["X-Forwarded-For"] = r.In.Header["X-Forwarded-For"]
			r.SetXForwarded()
		},
	}

	// Create the proxyProvider proxy
	//
	proxy.proxyProvider, err = proxyProvider.NewProxy(pcfg)
	if err != nil {
		return nil, fmt.Errorf("Error initializing proxy on proxyProvider: %w", err)
	}

	// Create reverse proxy server
	//
	handler := reverseProxyFunc(proxy.reverseProxy)

	proxy.log.Debug().
		Str("hostname", proxy.Config.Hostname).
		Msg("Proxy server created successfully")

	// add logger to proxy
	//
	if proxy.Config.ProxyAccessLog {
		handler = core.LoggerMiddleware(proxy.log, handler)
	}

	proxy.handler = handler

	return proxy, nil
}

// Close method is a method that closes the proxy.
func (proxy *Proxy) Close() error {
	var errs error
	proxy.log.Info().Str("name", proxy.Config.Hostname).Msg("stopping proxy")
	// if has http redirect server
	if proxy.httpListener != nil {
		errs = errors.Join(errs, proxy.httpListener.Close())
	}
	if proxy.listener != nil {
		errs = errors.Join(proxy.listener.Close())
	}
	if proxy.proxyProvider != nil {
		errs = errors.Join(proxy.proxyProvider.Close())
	}
	return errs
}

// Start method is a method that starts the proxy.
func (proxy *Proxy) Start() {
	proxy.log.Info().Str("name", proxy.Config.Hostname).Msg("starting proxy")
	var err error
	// Create the listener
	//
	proxy.listener, err = proxy.proxyProvider.GetTLSListener("tcp", ":443")
	if err != nil {
		proxy.log.Error().Err(err).Msg("Error Listening on TLS")
		proxy.Close()
		return
	}

	// start server
	//
	srv := &http.Server{
		Handler:           proxy.handler,
		ReadHeaderTimeout: core.ReadHeaderTimeout,
	}
	err = srv.Serve(proxy.listener)
	defer proxy.log.Printf("Terminating server %s", proxy.Config.Hostname)

	if err != nil && !errors.Is(err, net.ErrClosed) {
		proxy.log.Error().Err(err).Str("hostname", proxy.Config.Hostname).Msg("Error starting proxy server")
	}
}

// StartRedirectServer method is a method that starts http rediret server to https.
func (proxy *Proxy) StartRedirectServer() error {
	var err error
	proxy.httpListener, err = proxy.proxyProvider.GetListener("tcp", ":80")
	if err != nil {
		return fmt.Errorf("error creating HTTP listener: %w", err)
	}

	go func() {
		httpServer := &http.Server{
			ReadHeaderTimeout: core.ReadHeaderTimeout,
			Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				target := "https://" + r.Host + r.URL.RequestURI()
				http.Redirect(w, r, target, http.StatusMovedPermanently)
			}),
		}
		err := httpServer.Serve(proxy.httpListener)
		if err != nil && err != http.ErrServerClosed {
			// Log the error, but don't stop the main server
			fmt.Printf("HTTP redirect server error: %v\n", err)
		}
	}()

	return nil
}

// reverseProxyFunc func is a method that returns a reverse proxy handler.
func reverseProxyFunc(p *httputil.ReverseProxy) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p.ServeHTTP(w, r)
	})
}
