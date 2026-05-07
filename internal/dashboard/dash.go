// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package dashboard

import (
	"strconv"
	"strings"
	"sync"

	"github.com/almeidapaulopt/tsdproxy/internal/core"
	"github.com/almeidapaulopt/tsdproxy/internal/model"
	"github.com/almeidapaulopt/tsdproxy/internal/proxymanager"
	"github.com/almeidapaulopt/tsdproxy/internal/ui/pages"
	"github.com/almeidapaulopt/tsdproxy/web"

	"github.com/rs/zerolog"
)

type Dashboard struct {
	Log        zerolog.Logger
	HTTP       *core.HTTPServer
	pm         *proxymanager.ProxyManager
	sseClients map[string]*sseClient
	mtx        sync.RWMutex
}

func NewDashboard(http *core.HTTPServer, log zerolog.Logger, pm *proxymanager.ProxyManager) *Dashboard {
	dash := &Dashboard{
		Log:        log.With().Str("module", "dashboard").Logger(),
		HTTP:       http,
		pm:         pm,
		sseClients: make(map[string]*sseClient),
	}

	go dash.streamProxyUpdates()

	return dash
}

// AddRoutes method add dashboard related routes to the http server
func (dash *Dashboard) AddRoutes() {
	dash.HTTP.Get("/stream", dash.streamHandler())
	dash.HTTP.Get("/", web.Static)
}

// index is the HandlerFunc to index page of dashboard
func (dash *Dashboard) renderList(client *sseClient) {
	dash.mtx.RLock()
	defer dash.mtx.RUnlock()

	// force remove elements of proxy-list in case of client reconnect
	client.send(SSEMessage{
		Type:    EventClearList,
		Message: "#proxy-list",
	})

	proxies := dash.pm.GetProxies()
	for name, p := range proxies {
		if p.Config.Dashboard.Visible {
			dash.renderProxy(client, name, EventAppend)
		}
	}

	dash.streamSortList(client)
}

func (dash *Dashboard) renderProxy(client *sseClient, name string, ev EventType) {
	p, ok := dash.pm.GetProxy(name)
	if !ok {
		return
	}

	status := p.GetStatus()

	url := p.GetURL()
	if status == model.ProxyStatusAuthenticating {
		url = p.GetAuthURL()
	}

	icon := p.Config.Dashboard.Icon
	if icon == "" {
		icon = model.DefaultDashboardIcon
	}

	label := p.Config.Dashboard.Label
	if label == "" {
		label = name
	}

	hostname := strings.TrimPrefix(url, "https://")
	hostname = strings.TrimPrefix(hostname, "http://")

	ports := make([]pages.PortEntry, 0, len(p.Config.Ports))
	for _, target := range p.Config.Ports {
		scheme := "https"
		if target.ProxyProtocol == "http" {
			scheme = "http"
		}

		portURL := scheme + "://" + hostname
		if (scheme == "https" && target.ProxyPort != 443) || (scheme == "http" && target.ProxyPort != 80) {
			portURL += ":" + strconv.Itoa(target.ProxyPort)
		}

		ports = append(ports, pages.PortEntry{
			PortConfig: target,
			URL:        portURL,
		})
	}

	enabled := status == model.ProxyStatusAuthenticating || status == model.ProxyStatusRunning

	a := pages.ProxyData{
		Enabled:     enabled,
		Name:        name,
		URL:         url,
		ProxyStatus: status,
		Icon:        icon,
		Label:       label,
		Ports:       ports,
	}

	client.send(SSEMessage{
		Type: ev,
		Comp: pages.Proxy(a),
	})
}
