// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package dashboard

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

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

	// Ensure filter signals are initialized before proxy cards arrive,
	// so data-show expressions evaluate correctly on SSE-appended elements.
	client.send(SSEMessage{
		Type:    EventUpdateSignals,
		Message: `{"filterStatus":"all","filterHealth":"all"}`,
	})

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

		targetURL := target.GetFirstTarget().String()

		ports = append(ports, pages.PortEntry{
			PortConfig: target,
			URL:        portURL,
			TargetURL:  targetURL,
		})
	}

	authURL := ""
	if status == model.ProxyStatusAuthenticating {
		authURL = p.GetAuthURL()
	}

	enabled := status == model.ProxyStatusAuthenticating || status == model.ProxyStatusRunning

	health := p.GetHealth()
	healthStatus := health.Status.String()
	healthLatency := ""
	if health.Status == 0 {
		healthStatus = ""
	} else if health.Latency > 0 {
		healthLatency = fmt.Sprintf("(%dms)", health.Latency.Milliseconds())
	}

	statusHistory := p.GetStatusHistory()
	history := make([]pages.StatusHistoryEntry, len(statusHistory))
	for i, t := range statusHistory {
		history[i] = pages.StatusHistoryEntry{
			Status:    t.Status.String(),
			Timestamp: t.Timestamp.Format(time.RFC3339),
			When:      formatAgo(t.Timestamp),
		}
	}

	uptime := formatDuration(p.GetUptime())

	a := pages.ProxyData{
		Enabled:               enabled,
		Name:                  name,
		URL:                   url,
		ProxyStatus:           status,
		Icon:                  icon,
		Label:                 label,
		Ports:                 ports,
		Hostname:              hostname,
		TargetProvider:        p.Config.TargetProvider,
		TargetID:              p.Config.TargetID,
		TargetImage:           p.Config.TargetImage,
		TailscaleTags:         p.Config.Tailscale.Tags,
		TailscaleEphemeral:    p.Config.Tailscale.Ephemeral,
		TailscaleRunWebClient: p.Config.Tailscale.RunWebClient,
		ProxyAccessLog:        p.Config.ProxyAccessLog,
		AuthURL:               authURL,
		HealthStatus:          healthStatus,
		HealthLatency:         healthLatency,
		Category:              p.Config.Dashboard.Category,
		StatusHistory:         history,
		Uptime:                uptime,
	}

	client.send(SSEMessage{
		Type: ev,
		Comp: pages.Proxy(a),
	})
}

func formatAgo(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		m := int(math.Round(d.Minutes()))
		if m == 1 {
			return "1m ago"
		}
		return fmt.Sprintf("%dm ago", m)
	case d < 24*time.Hour:
		h := int(math.Round(d.Hours()))
		if h == 1 {
			return "1h ago"
		}
		return fmt.Sprintf("%dh ago", h)
	default:
		days := int(math.Round(d.Hours() / 24))
		if days == 1 {
			return "1d ago"
		}
		return fmt.Sprintf("%dd ago", days)
	}
}

func formatDuration(d time.Duration) string {
	if d == 0 {
		return ""
	}
	days := int(d.Hours() / 24)
	hours := int(math.Mod(d.Hours(), 24))
	minutes := int(math.Mod(d.Minutes(), 60))

	var parts []string
	if days > 0 {
		parts = append(parts, fmt.Sprintf("%dd", days))
	}
	if hours > 0 {
		parts = append(parts, fmt.Sprintf("%dh", hours))
	}
	if minutes > 0 || len(parts) == 0 {
		parts = append(parts, fmt.Sprintf("%dm", minutes))
	}
	return strings.Join(parts, " ")
}
