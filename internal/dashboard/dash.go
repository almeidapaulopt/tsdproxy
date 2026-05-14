// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package dashboard

import (
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/almeidapaulopt/tsdproxy/internal/dom"
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
	whoisFunc := func(r *http.Request) model.Whois {
		who, _ := model.WhoisFromContext(r.Context())
		return who
	}

	adminMW := core.AdminMiddleware(whoisFunc)

	dash.HTTP.Get("/stream", dash.streamHandler())
	dash.HTTP.Get("/stream/{name}/logs", dash.streamProxyLogsHandler())
	dash.HTTP.Get("/", web.Static)

	dash.HTTP.Post("/api/proxy/{name}/restart", adminMW(dash.restartHandler()))
	dash.HTTP.Post("/api/proxy/{name}/pause", adminMW(dash.pauseHandler()))
	dash.HTTP.Post("/api/proxy/{name}/resume", adminMW(dash.resumeHandler()))
	dash.HTTP.Post("/api/proxy/{name}/reauth", adminMW(dash.reauthHandler()))
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

func (dash *Dashboard) proxyActionHandler(action func(string) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		if name == "" {
			dash.writeJSONError(w, "invalid proxy name", http.StatusBadRequest)
			return
		}

		if err := action(name); err != nil {
			dash.writeJSONError(w, err.Error(), http.StatusBadRequest)
			return
		}

		dash.HTTP.JSONResponse(w, r, map[string]string{"status": "ok"})
	}
}

func (dash *Dashboard) restartHandler() http.HandlerFunc {
	return dash.proxyActionHandler(dash.pm.RestartProxy)
}

func (dash *Dashboard) pauseHandler() http.HandlerFunc {
	return dash.proxyActionHandler(dash.pm.PauseProxy)
}

func (dash *Dashboard) resumeHandler() http.HandlerFunc {
	return dash.proxyActionHandler(dash.pm.ResumeProxy)
}

func (dash *Dashboard) reauthHandler() http.HandlerFunc {
	return dash.restartHandler()
}

func (dash *Dashboard) writeJSONError(w http.ResponseWriter, message string, code int) {
	dash.HTTP.JSONResponseCode(w, nil, map[string]any{"message": message, "code": code}, code)
}

func (dash *Dashboard) streamProxyLogsHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		if name == "" {
			http.Error(w, "invalid proxy name", http.StatusBadRequest)
			return
		}

		proxy, ok := dash.pm.GetProxy(name)
		if !ok {
			http.Error(w, "proxy not found", http.StatusNotFound)
			return
		}

		if !proxy.Config.Dashboard.Visible {
			http.Error(w, "proxy not found", http.StatusNotFound)
			return
		}

		snapshot, ch := proxy.SubscribeLogs()
		if ch == nil {
			http.Error(w, "no log buffer", http.StatusNotFound)
			return
		}
		defer proxy.UnsubscribeLogs(ch)

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		if _, ok := w.(http.Flusher); !ok {
			http.Error(w, "streaming not supported", http.StatusInternalServerError)
			return
		}
		safeID := dom.SafeID(name)
		selector := "#log-lines-" + safeID
		scrollSelector := selector
		trimSelector := selector
		maxLines := fmt.Sprintf("%d", proxymanager.DefaultLogBufferSize)

		dash.Log.Debug().Str("proxy", name).Msg("log stream client connected")

		if err := SSEClearList(w, selector); err != nil {
			return
		}

		if len(snapshot) > 0 {
			if err := SSEAppendHTML(w, pages.LogLines(snapshot)); err != nil {
				return
			}
			if err := WriteSSE(w, "scroll-logs", scrollSelector); err != nil {
				return
			}
		} else {
			if err := SSEAppendHTML(w, fmt.Sprintf(`<div id="log-placeholder-%s" class="%s">%s</div>`, safeID, pages.LogLineClasses()+pages.LogPlaceholderExtra(), pages.LogPlaceholderText())); err != nil {
				return
			}
		}

		placeholderRemoved := len(snapshot) > 0

		for {
			select {
			case <-r.Context().Done():
				return
			case line, ok := <-ch:
				if !ok {
					return
				}
				if !placeholderRemoved {
					if err := SSERemoveElement(w, "#log-placeholder-"+safeID); err != nil {
						return
					}
					placeholderRemoved = true
				}

				const maxBatchSize = 50

				lines := []string{line}
			drain:
				for len(lines) < maxBatchSize {
					select {
					case l, ok := <-ch:
						if !ok {
							break drain
						}
						lines = append(lines, l)
					default:
						break drain
					}
				}

				if err := SSEAppendHTML(w, pages.LogLines(lines)); err != nil {
					return
				}
				if err := WriteSSE(w, "trim-logs", trimSelector+"\n"+maxLines); err != nil {
					return
				}
				if err := WriteSSE(w, "scroll-logs", scrollSelector); err != nil {
					return
				}
			}
		}
	}
}
