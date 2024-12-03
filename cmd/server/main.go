// SPDX-FileCopyrightText: 2024 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package main

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/docker/docker/client"
	"github.com/rs/zerolog"

	"github.com/almeidapaulopt/tsdproxy/internal/config"
	"github.com/almeidapaulopt/tsdproxy/internal/core"
	"github.com/almeidapaulopt/tsdproxy/internal/dashboard"
	pm "github.com/almeidapaulopt/tsdproxy/internal/proxymanager"
)

type WebApp struct {
	Log          zerolog.Logger
	HTTP         *core.HTTPServer
	Health       *core.Health
	Docker       *client.Client
	ProxyManager *pm.ProxyManager
	Dashboard    *dashboard.Dashboard
}

func InitializeApp() (*WebApp, error) {
	err := config.InitializeConfig()
	if err != nil {
		return nil, err
	}
	logger := core.NewLog()
	httpServer := core.NewHTTPServer(logger)
	health := core.NewHealthHandler(httpServer, logger)

	// Start ProxyManager
	//
	proxymanager := pm.NewProxyManager(logger)

	// init Dashboard
	//
	dash := dashboard.NewDashboard(httpServer, logger, proxymanager.Proxies)

	webApp := &WebApp{
		Log:          logger,
		HTTP:         httpServer,
		Health:       health,
		ProxyManager: proxymanager,
		Dashboard:    dash,
	}
	return webApp, nil
}

func main() {
	println("Initializing server")
	println("Version", core.GetVersion())

	app, err := InitializeApp()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	app.Start()
	defer app.Stop()

	// Wait for interrupt signal to gracefully shutdown the server with a timeout of 10 seconds.
	//
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)
	<-quit
}

func (app *WebApp) Start() {
	app.Log.Info().
		Str("Version", core.GetVersion()).Msg("Starting server")

	// Start the webserver
	//
	go func() {
		app.Log.Info().Msg("Initializing WebServer")

		// Start the webserver
		//
		srv := http.Server{
			Addr:              fmt.Sprintf("%s:%d", config.Config.HTTP.Hostname, config.Config.HTTP.Port),
			ReadHeaderTimeout: core.ReadHeaderTimeout,
		}

		app.Health.SetReady()

		if err := app.HTTP.StartServer(&srv); errors.Is(err, http.ErrServerClosed) {
			app.Log.Fatal().Err(err).Msg("shutting down the server")
		}
	}()

	// Setup proxy for existing containers
	//
	app.Log.Info().Msg("Setting up proxy proxies")

	app.ProxyManager.Start()

	// Start watching docker events
	//
	go app.ProxyManager.WatchEvents()

	// Start Dashboard
	//
	app.Dashboard.AddRoutes()
}

func (app *WebApp) Stop() {
	app.Log.Info().Msg("Shutdown server")

	app.Health.SetNotReady()

	// Shutdown things here
	//
	app.ProxyManager.StopAllProxies()

	app.Log.Info().Msg("Server was shutdown successfully")
}
