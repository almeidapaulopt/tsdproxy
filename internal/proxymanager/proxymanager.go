package proxymanager

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"sync"

	"github.com/rs/zerolog/log"

	ctypes "github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/events"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"

	"github.com/almeidapaulopt/tsdproxy/internal/containers"
	"github.com/almeidapaulopt/tsdproxy/internal/core"
	"github.com/almeidapaulopt/tsdproxy/internal/tailscale"
)

type ProxyManager struct {
	proxies map[string]*Proxy
	docker  *client.Client
	Log     *core.Logger
	config  *core.Config
	mutex   sync.Mutex
}

type Proxy struct {
	TsServer     *tailscale.TsNetServer
	reverseProxy *httputil.ReverseProxy
	container    *containers.Container
	URL          *url.URL
}

func NewProxyManager(cli *client.Client, logger *core.Logger, config *core.Config) *ProxyManager {
	return &ProxyManager{
		proxies: make(map[string]*Proxy),
		docker:  cli,
		config:  config,
		Log:     logger,
	}
}

func (pm *ProxyManager) AddProxy(proxy *Proxy) {
	pm.mutex.Lock()
	defer pm.mutex.Unlock()

	pm.proxies[proxy.container.ID] = proxy
}

func (pm *ProxyManager) RemoveProxy(containerID string) {
	pm.mutex.Lock()
	defer pm.mutex.Unlock()

	if proxy, exists := pm.proxies[containerID]; exists {
		if err := proxy.TsServer.Close(); err != nil {
			pm.Log.Error().Err(err).Str("containerID", containerID).Msg("Error shutting down proxy server")
		} else {
			pm.Log.Info().Str("containerID", containerID).Msg("Proxy server shut down successfully")
		}

		delete(pm.proxies, containerID)
		pm.Log.Info().Str("containerID", containerID[:12]).Msg("Removed proxy for container")
	}
}

func (pm *ProxyManager) SetupExistingContainers(ctx context.Context) error {
	// Filter containers with enable set to true
	//
	containerFilter := filters.NewArgs()
	containerFilter.Add("label", containers.LabelIsEnabled)

	containers, err := pm.docker.ContainerList(ctx, ctypes.ListOptions{
		Filters: containerFilter,
		All:     false,
	})
	if err != nil {
		pm.Log.Error().Err(err).Msg("error listing containers")
		return fmt.Errorf("error listing containers: %w", err)
	}

	// add proxies to existing Containers
	//
	for _, container := range containers {
		go pm.SetupProxy(ctx, container.ID)
	}

	return nil
}

func (pm *ProxyManager) HandleContainerEvent(ctx context.Context, event events.Message) {
	containerID := event.Actor.ID

	pm.Log.Debug().Str("containerID", containerID).Str("event", string(event.Action)).Msg("Handling container event")

	switch event.Action {
	case events.ActionStart:
		go pm.SetupProxy(ctx, containerID)
	case events.ActionDie:
		pm.RemoveProxy(containerID)
	}
}

func (pm *ProxyManager) SetupProxy(ctx context.Context, containerID string) {
	pm.Log.Info().Str("containerID", containerID).Msg("setting up proxy for container")

	// Create a new container
	//
	container, err := containers.NewContainer(ctx, containerID, pm.docker, pm.config.Hostname)
	if err != nil {
		pm.Log.Error().Err(err).Str("containerID", containerID).Msg("Error creating container")
		return
	}

	// Get the proxy URL
	//
	proxyURL, err := container.GetProxyURL()
	if err != nil {
		pm.Log.Error().Err(err).Str("containerID", container.ID).Str("containerName", container.GetName()).Msg("Error parsing hostname")
		return
	}

	// Get the target URL
	//
	targetURL, err := container.GetTargetURL()
	if err != nil {
		pm.Log.Error().Err(err).Str("containerID", containerID).Str("containerName", container.GetName()).Msg("error on proxy URL")
		return
	}

	pm.Log.Debug().
		Str("containerID", containerID).
		Str("containerName", container.GetName()).
		Str("targetURL", targetURL.String()).
		Str("proxyURL", proxyURL.String()).
		Msg("initializing proxy for container")

	// Create the reverse proxy
	//
	reverseProxy := httputil.NewSingleHostReverseProxy(targetURL)

	// Create the tsnet server
	//
	server := tailscale.NewTsNetServer(proxyURL.Hostname(), pm.config, pm.Log)
	defer server.Close()

	if err := server.Start(ctx); err != nil {
		pm.Log.Error().Err(err).Str("containerID", containerID).Str("containerName", container.GetName()).Msg("Error starting server")
		return
	}

	// Create the TLS listener
	//
	ln, err := server.TsServer.ListenTLS("tcp", ":443")
	if err != nil {
		pm.Log.Error().Err(err).Str("containerID", containerID).Str("containerName", container.GetName()).Msg("Error listening on TLS")
		return
	}
	defer ln.Close()

	// AddProxy to the list
	//
	pm.AddProxy(&Proxy{
		container:    container,
		TsServer:     server,
		URL:          proxyURL,
		reverseProxy: reverseProxy,
	})

	// Create reverse proxy server
	//
	handler := pm.reverseProxyFunc(reverseProxy)

	pm.Log.Debug().
		Str("containerID", containerID).
		Str("containerName", container.GetName()).
		Msg("Proxy server created successfully")

	// add logger to proxy
	//
	if pm.config.ContainerAccessLog {
		handler = pm.Log.LoggerMiddleware(handler)
	}

	// start server
	//
	err = http.Serve(ln, handler)
	defer log.Printf("Terminating server %s", proxyURL.Hostname())

	if err != nil && !errors.Is(err, net.ErrClosed) {
		pm.Log.Error().Err(err).Str("containerID", containerID[:12]).Msg("Error starting proxy server for container")
	}
}

func (pm *ProxyManager) WatchDockerEvents(ctx context.Context) {
	// Filter Start/stop events for containers
	//
	eventsFilter := filters.NewArgs()
	eventsFilter.Add("label", containers.LabelIsEnabled)
	eventsFilter.Add("type", string(events.ContainerEventType))
	eventsFilter.Add("event", string(events.ActionDie))
	eventsFilter.Add("event", string(events.ActionStart))

	eventsChan, errChan := pm.docker.Events(ctx, events.ListOptions{
		Filters: eventsFilter,
	})

	for {
		select {
		case event := <-eventsChan:
			pm.HandleContainerEvent(ctx, event)
		case err := <-errChan:
			log.Error().Err(err).Msg("Error watching Docker events")
			return
		}
	}
}

func (pm *ProxyManager) StopAll() {
	pm.Log.Info().Msg("Shutdown all proxies")
	for id := range pm.proxies {
		pm.RemoveProxy(id)
	}
}

func (pm *ProxyManager) reverseProxyFunc(p *httputil.ReverseProxy) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p.ServeHTTP(w, r)
	})
}
