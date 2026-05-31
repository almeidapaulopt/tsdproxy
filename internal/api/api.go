// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package api

import (
	"fmt"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/almeidapaulopt/tsdproxy/internal/config"
	"github.com/almeidapaulopt/tsdproxy/internal/core"
	"github.com/almeidapaulopt/tsdproxy/internal/core/webhook"
	"github.com/almeidapaulopt/tsdproxy/internal/model"
	"github.com/almeidapaulopt/tsdproxy/internal/proxymanager"

	"github.com/rs/zerolog"
)

type API struct {
	HTTP *core.HTTPServer
	PM   *proxymanager.ProxyManager
	Log  zerolog.Logger
}

func New(http *core.HTTPServer, pm *proxymanager.ProxyManager, log zerolog.Logger) *API {
	return &API{
		HTTP: http,
		PM:   pm,
		Log:  log.With().Str("module", "api").Logger(),
	}
}

func (a *API) AddRoutes() {
	authMW := core.AdminMiddleware()

	a.HTTP.Get("/api/v1/proxies", authMW(a.listProxiesHandler()))
	a.HTTP.Get("/api/v1/proxies/{name}", authMW(a.getProxyHandler()))
	a.HTTP.Get("/api/v1/proxies/{name}/ports", authMW(a.getProxyPortsHandler()))
	a.HTTP.Get("/api/version", authMW(a.versionHandler()))
	a.HTTP.Get("/api/health", authMW(a.aggregateHealthHandler()))
	a.HTTP.Get("/api/whoami", authMW(core.WhoAmIHandler(a.HTTP)))
	a.HTTP.Post("/api/webhook/test", authMW(a.testWebhookHandler()))
}

type (
	apiProxiesResponse struct {
		Proxies []apiProxy `json:"proxies"`
	}

	apiProxy struct {
		Category       string       `json:"category,omitempty"`
		TargetID       string       `json:"targetId"`
		Status         string       `json:"status"`
		Health         string       `json:"health"`
		HealthLatency  string       `json:"healthLatency,omitempty"`
		URL            string       `json:"url"`
		TargetImage    string       `json:"targetImage,omitempty"`
		Name           string       `json:"name"`
		Label          string       `json:"label"`
		Uptime         string       `json:"uptime,omitempty"`
		TargetProvider string       `json:"targetProvider"`
		StatusHistory  []apiStatus  `json:"statusHistory,omitempty"`
		Tailscale      apiTailscale `json:"tailscale"`
		Ports          []apiPort    `json:"ports"`
	}

	apiPort struct {
		Name          string `json:"name"`
		ProxyProtocol string `json:"proxyProtocol"`
		TargetURL     string `json:"targetUrl"`
		ProxyPort     int    `json:"proxyPort"`
		TLSValidate   bool   `json:"tlsValidate"`
		IsRedirect    bool   `json:"isRedirect"`
		Funnel        bool   `json:"funnel"`
	}

	apiTailscale struct {
		Tags         string `json:"tags,omitempty"`
		Ephemeral    bool   `json:"ephemeral"`
		RunWebClient bool   `json:"runWebClient"`
	}

	apiStatus struct {
		Status    string `json:"status"`
		Timestamp string `json:"timestamp"`
	}

	apiVersionResponse struct {
		Version string `json:"version"`
		Author  string `json:"author"`
		IsDirty bool   `json:"isDirty"`
	}

	apiHealthResponse struct {
		Total     int `json:"total"`
		Running   int `json:"running"`
		Stopped   int `json:"stopped"`
		Error     int `json:"error"`
		Paused    int `json:"paused"`
		Unhealthy int `json:"unhealthy"`
	}

	apiErrorResponse struct {
		Message string `json:"message"`
		Code    int    `json:"code"`
	}
)

func (a *API) listProxiesHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		proxies := a.PM.GetProxies()
		items := make([]apiProxy, 0, len(proxies))
		for name, p := range proxies {
			if !p.Config.Dashboard.Visible {
				continue
			}
			items = append(items, a.toAPIProxy(name, p))
		}
		a.HTTP.JSONResponse(w, r, apiProxiesResponse{Proxies: items})
	}
}

func (a *API) getProxyHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		if name == "" {
			a.HTTP.ErrorResponse(w, r, nil, "missing proxy name", http.StatusBadRequest)
			return
		}

		p, ok := a.PM.GetProxy(name)
		if !ok || !p.Config.Dashboard.Visible {
			a.HTTP.ErrorResponse(w, r, nil, "proxy not found", http.StatusNotFound)
			return
		}

		a.HTTP.JSONResponse(w, r, a.toAPIProxy(name, p))
	}
}

func (a *API) getProxyPortsHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		if name == "" {
			a.HTTP.ErrorResponse(w, r, nil, "missing proxy name", http.StatusBadRequest)
			return
		}

		p, ok := a.PM.GetProxy(name)
		if !ok || !p.Config.Dashboard.Visible {
			a.HTTP.ErrorResponse(w, r, nil, "proxy not found", http.StatusNotFound)
			return
		}

		a.HTTP.JSONResponse(w, r, struct {
			Ports []apiPort `json:"ports"`
		}{Ports: a.toAPIPorts(p)})
	}
}

func (a *API) versionHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		a.HTTP.JSONResponse(w, r, apiVersionResponse{
			Version: core.GetVersion(),
			Author:  core.AppAuthor,
			IsDirty: core.GetIsDirty(),
		})
	}
}

func (a *API) aggregateHealthHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		proxies := a.PM.GetProxies()
		result := apiHealthResponse{}

		for _, p := range proxies {
			result.Total++
			switch p.GetStatus() {
			case model.ProxyStatusRunning:
				result.Running++
			case model.ProxyStatusStopped:
				result.Stopped++
			case model.ProxyStatusError:
				result.Error++
			case model.ProxyStatusPaused:
				result.Paused++
			}
			if p.GetHealth().Status == proxymanager.HealthDown {
				result.Unhealthy++
			}
		}

		a.HTTP.JSONResponse(w, r, result)
	}
}

func (a *API) toAPIProxy(name string, p *proxymanager.Proxy) apiProxy {
	status := p.GetStatus()

	url := p.GetURL()
	if status == model.ProxyStatusAuthenticating || status == model.ProxyStatusAwaitingApproval {
		url = p.GetAuthURL()
	}

	label := p.Config.Dashboard.Label
	if label == "" {
		label = name
	}

	health := p.GetHealth()
	healthStatus := health.Status.String()
	var healthLatency string
	if health.Latency > 0 {
		healthLatency = fmt.Sprintf("%dms", health.Latency.Milliseconds())
	}

	rawHistory := p.GetStatusHistory()
	history := make([]apiStatus, len(rawHistory))
	for i, t := range rawHistory {
		history[i] = apiStatus{
			Status:    t.Status.String(),
			Timestamp: t.Timestamp.Format(time.RFC3339),
		}
	}

	return apiProxy{
		Name:          name,
		Label:         label,
		Status:        status.String(),
		Health:        healthStatus,
		HealthLatency: healthLatency,
		URL:           url,
		Category:      p.Config.Dashboard.Category,
		Uptime:        formatDuration(p.GetUptime()),
		Ports:         a.toAPIPorts(p),
		Tailscale: apiTailscale{
			Tags:         p.Config.Tailscale.Tags,
			Ephemeral:    p.Config.Tailscale.Ephemeral,
			RunWebClient: p.Config.Tailscale.RunWebClient,
		},
		StatusHistory:  history,
		TargetProvider: p.Config.TargetProvider,
		TargetID:       p.Config.TargetID,
		TargetImage:    p.Config.TargetImage,
	}
}

func (a *API) toAPIPorts(p *proxymanager.Proxy) []apiPort {
	ports := make([]apiPort, 0, len(p.Config.Ports))
	for k, v := range p.Config.Ports {
		ports = append(ports, apiPort{
			Name:          k,
			ProxyProtocol: v.ProxyProtocol,
			ProxyPort:     v.ProxyPort,
			TargetURL:     v.GetFirstTarget().String(),
			TLSValidate:   v.TLSValidate,
			IsRedirect:    v.IsRedirect,
			Funnel:        v.Tailscale.Funnel,
		})
	}
	return ports
}

func (a *API) testWebhookHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if len(config.Config.Webhooks) == 0 {
			a.writeJSONError(w, "no webhooks configured", http.StatusBadRequest)
			return
		}

		running := model.ProxyStatusRunning
		stopped := model.ProxyStatusStopped

		sender := webhook.NewSender(a.Log, config.Config.Webhooks)
		defer sender.Close()

		if err := sender.SendSync(webhook.Event{
			ProxyName: "test-proxy",
			Status:    running.String(),
			OldStatus: stopped.String(),
			Timestamp: time.Now().UTC().Format(time.RFC3339),
			Message:   "Test webhook from TSDProxy",
		}); err != nil {
			a.writeJSONError(w, "webhook test failed: "+err.Error(), http.StatusBadGateway)
			return
		}

		a.HTTP.JSONResponse(w, r, map[string]string{
			"message": "test webhook sent",
		})
	}
}

func (a *API) writeJSONError(w http.ResponseWriter, message string, code int) {
	a.HTTP.JSONResponseCode(w, nil, apiErrorResponse{Message: message, Code: code}, code)
}

func formatDuration(d time.Duration) string {
	if d == 0 {
		return ""
	}
	days := int(d.Hours() / 24)               //nolint:mnd
	hours := int(math.Mod(d.Hours(), 24))     //nolint:mnd
	minutes := int(math.Mod(d.Minutes(), 60)) //nolint:mnd

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
