// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/moby/moby/client"
	"github.com/rs/zerolog"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	"github.com/almeidapaulopt/tsdproxy/internal/api"
	"github.com/almeidapaulopt/tsdproxy/internal/config"
	"github.com/almeidapaulopt/tsdproxy/internal/core"
	"github.com/almeidapaulopt/tsdproxy/internal/dashboard"
	pm "github.com/almeidapaulopt/tsdproxy/internal/proxymanager"
	"github.com/almeidapaulopt/tsdproxy/web"
)

const (
	dirPermission   fs.FileMode = 0o700
	filePermission  fs.FileMode = 0o600
	shutdownTimeout             = 10 * time.Second
)

type WebApp struct {
	Log            zerolog.Logger
	HTTP           *core.HTTPServer
	Health         *core.Health
	Docker         *client.Client
	ProxyManager   *pm.ProxyManager
	Dashboard      *dashboard.Dashboard
	httpServer     *http.Server
	tracerProvider *sdktrace.TracerProvider
}

func InitializeApp() (*WebApp, error) {
	err := config.InitializeConfig()
	if err != nil {
		return nil, err
	}
	config.Config.ClearSecrets()
	config.Config.LoadTailscaleEnvOverrides()

	// Write HTTP port to data dir for the healthcheck binary to read.
	portFile := filepath.Join(config.Config.Tailscale.DataDir, ".http-port")
	if err := os.MkdirAll(config.Config.Tailscale.DataDir, dirPermission); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}
	if err := os.WriteFile(portFile, []byte(strconv.FormatUint(uint64(config.Config.HTTP.Port), 10)), filePermission); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to write healthcheck port file: %v\n", err)
	}

	logger := core.NewLog()

	core.InitProxyAuth(logger)

	httpServer := core.NewHTTPServer(logger)
	httpServer.Use(core.StripProxyIdentityHeaders)
	httpServer.Use(core.SessionMiddleware)
	httpServer.Use(core.CSRFMiddleware)

	health := core.NewHealthHandler(httpServer, logger)

	// Start ProxyManager
	//
	proxymanager := pm.NewProxyManager(logger)

	// init Dashboard
	//
	dash := dashboard.NewDashboard(httpServer, logger, proxymanager)

	var tracerProvider *sdktrace.TracerProvider
	if config.Config.Telemetry.Enabled {
		tp, err := core.InitTracer(context.Background(), config.Config.Telemetry.Endpoint, config.Config.Telemetry.Insecure)
		if err != nil {
			logger.Error().Err(err).Msg("failed to initialize tracer")
		} else {
			tracerProvider = tp
			logger.Info().Str("endpoint", config.Config.Telemetry.Endpoint).Msg("OpenTelemetry tracer initialized")
		}
	}

	webApp := &WebApp{
		Log:            logger,
		HTTP:           httpServer,
		Health:         health,
		ProxyManager:   proxymanager,
		Dashboard:      dash,
		tracerProvider: tracerProvider,
	}
	return webApp, nil
}

func main() {
	fmt.Fprintf(os.Stderr, "Initializing server\nVersion %s\n", core.GetVersion())

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
	// Add Routes
	//
	app.Dashboard.AddRoutes()
	api.New(app.HTTP, app.ProxyManager, app.Log).AddRoutes()

	// Static assets from embedded dist/ (CSS, JS, icons, etc.)
	app.HTTP.Mux.Handle("/", web.Static)

	adminMW := core.AdminMiddleware()
	app.HTTP.Get("/metrics", adminMW(app.ProxyManager.MetricsHandler()))
	// Setup proxy for existing containers
	//
	app.Log.Info().Msg("Setting up proxy proxies")

	app.ProxyManager.Start()

	// Start watching docker events
	//
	app.ProxyManager.WatchEvents()

	// Start the webserver
	//
	go func() {
		app.Log.Info().Msg("Initializing WebServer")

		addr := fmt.Sprintf("%s:%d", config.Config.HTTP.Hostname, config.Config.HTTP.Port)

		ln, err := net.Listen("tcp", addr)
		if err != nil {
			app.Log.Fatal().Err(err).Msg("failed to bind listener")
		}

		srv := http.Server{
			Handler:           core.LoggerMiddleware(app.Log, app.HTTP.Mux),
			Addr:              addr,
			ReadHeaderTimeout: core.ReadHeaderTimeout,
		}
		app.httpServer = &srv

		app.Health.SetReady()

		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			app.Log.Fatal().Err(err).Msg("shutting down the server")
		}
	}()
}

func (app *WebApp) Stop() {
	app.Log.Info().Msg("Shutdown server")

	app.Health.SetNotReady()

	if app.httpServer != nil {
		ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		if err := app.httpServer.Shutdown(ctx); err != nil {
			app.Log.Error().Err(err).Msg("HTTP server forced shutdown")
		}
	}

	// Shutdown things here
	//
	if app.Dashboard != nil {
		app.Dashboard.Close()
	}
	app.ProxyManager.StopAllProxies()

	if app.tracerProvider != nil {
		if err := app.tracerProvider.Shutdown(context.Background()); err != nil {
			app.Log.Error().Err(err).Msg("error shutting down tracer provider")
		}
	}

	app.Log.Info().Msg("Server was shutdown successfully")
}
