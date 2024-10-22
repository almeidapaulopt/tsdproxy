//  SPDX-FileCopyrightText: 2024 Paulo Almeida <almeidapaulopt@gmail.com>
//  SPDX-License-Identifier: MIT

package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/docker/docker/client"

	"github.com/almeidapaulopt/tsdproxy/internal/core"
	pm "github.com/almeidapaulopt/tsdproxy/internal/proxymanager"
)

type WebApp struct {
	Config       *core.Config
	Log          *core.Logger
	HTTP         *core.HTTPServer
	Health       *core.Health
	Docker       *client.Client
	ProxyManager *pm.ProxyManager
}

func InitializeApp() (*WebApp, error) {
	config, err := core.GetConfig()
	if err != nil {
		return nil, err
	}
	logger := core.NewLog(config)
	httpServer := core.NewHTTPServer(logger)
	health := core.NewHealthHandler(httpServer, logger)

	// Docker client
	//
	docker, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		logger.Fatal().Err(err).Msg("Error creating Docker client")
	}

	// Start ProxyManager
	//
	proxymanager := pm.NewProxyManager(docker, logger, config)

	webApp := &WebApp{
		Config:       config,
		Log:          logger,
		HTTP:         httpServer,
		Health:       health,
		Docker:       docker,
		ProxyManager: proxymanager,
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

	ctx := context.Background()

	// Setup proxy for existing containers
	//
	app.Log.Info().Msg("Setting up proxy for existing containers")
	if err := app.ProxyManager.SetupExistingContainers(ctx); err != nil {
		app.Log.Fatal().Err(err).Msg("Error setting up existing containers")
	}

	go app.ProxyManager.WatchDockerEvents(ctx)

	// Start the webserver
	//
	go func() {
		app.Log.Info().Msg("Initializing WebServer")

		// Start the webserver
		//
		srv := http.Server{
			Addr:              fmt.Sprintf("%s:%d", app.Config.HTTP.Hostname, app.Config.HTTP.Port),
			ReadHeaderTimeout: core.ReadHeaderTimeout,
		}

		app.Health.SetReady()

		if err := app.HTTP.StartServer(&srv); errors.Is(err, http.ErrServerClosed) {
			app.Log.Fatal().Err(err).Msg("shutting down the server")
		}
	}()
}

func (app *WebApp) Stop() {
	app.Log.Info().Msg("Shutdown server")

	app.Health.SetNotReady()

	// Shutdown things here
	//
	app.ProxyManager.StopAll()

	app.Log.Info().Msg("Server was shutdown successfully")
}
