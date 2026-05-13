// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package dashboard

import (
	"fmt"
	"net/http"
	"time"

	"github.com/almeidapaulopt/tsdproxy/internal/config"
	"github.com/almeidapaulopt/tsdproxy/internal/core"
	"github.com/almeidapaulopt/tsdproxy/internal/core/webhook"
	"github.com/almeidapaulopt/tsdproxy/internal/model"
	"github.com/almeidapaulopt/tsdproxy/internal/proxymanager"
)

type (
	apiProxiesResponse struct {
		Proxies []apiProxy `json:"proxies"`
	}

	apiProxy struct {
		Name           string       `json:"name"`
		Label          string       `json:"label"`
		Status         string       `json:"status"`
		Health         string       `json:"health"`
		HealthLatency  string       `json:"healthLatency,omitempty"`
		URL            string       `json:"url"`
		Category       string       `json:"category,omitempty"`
		Uptime         string       `json:"uptime,omitempty"`
		Ports          []apiPort    `json:"ports"`
		Tailscale      apiTailscale `json:"tailscale"`
		StatusHistory  []apiStatus  `json:"statusHistory,omitempty"`
		TargetProvider string       `json:"targetProvider"`
		TargetID       string       `json:"targetId"`
		TargetImage    string       `json:"targetImage,omitempty"`
	}

	apiPort struct {
		Name          string `json:"name"`
		ProxyProtocol string `json:"proxyProtocol"`
		ProxyPort     int    `json:"proxyPort"`
		TargetURL     string `json:"targetUrl"`
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

// AddAPIRoutes registers the public REST JSON API endpoints.
func (dash *Dashboard) AddAPIRoutes() {
	dash.HTTP.Get("/api/v1/proxies", dash.listProxiesHandler())
	dash.HTTP.Get("/api/v1/proxies/{name}", dash.getProxyHandler())
	dash.HTTP.Get("/api/v1/proxies/{name}/ports", dash.getProxyPortsHandler())
	dash.HTTP.Get("/api/version", dash.versionHandler())
	dash.HTTP.Get("/api/health", dash.aggregateHealthHandler())

	dash.HTTP.Post("/api/webhook/test", dash.testWebhookHandler())
}

func (dash *Dashboard) listProxiesHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		proxies := dash.pm.GetProxies()
		items := make([]apiProxy, 0, len(proxies))
		for name, p := range proxies {
			if !p.Config.Dashboard.Visible {
				continue
			}
			items = append(items, dash.toAPIProxy(name, p))
		}
		dash.HTTP.JSONResponse(w, r, apiProxiesResponse{Proxies: items})
	}
}

func (dash *Dashboard) getProxyHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		if name == "" {
			dash.HTTP.ErrorResponse(w, r, nil, "missing proxy name", http.StatusBadRequest)
			return
		}

		p, ok := dash.pm.GetProxy(name)
		if !ok || !p.Config.Dashboard.Visible {
			dash.HTTP.ErrorResponse(w, r, nil, "proxy not found", http.StatusNotFound)
			return
		}

		dash.HTTP.JSONResponse(w, r, dash.toAPIProxy(name, p))
	}
}

func (dash *Dashboard) getProxyPortsHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		if name == "" {
			dash.HTTP.ErrorResponse(w, r, nil, "missing proxy name", http.StatusBadRequest)
			return
		}

		p, ok := dash.pm.GetProxy(name)
		if !ok || !p.Config.Dashboard.Visible {
			dash.HTTP.ErrorResponse(w, r, nil, "proxy not found", http.StatusNotFound)
			return
		}

		dash.HTTP.JSONResponse(w, r, struct {
			Ports []apiPort `json:"ports"`
		}{Ports: dash.toAPIPorts(p)})
	}
}

func (dash *Dashboard) versionHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		dash.HTTP.JSONResponse(w, r, apiVersionResponse{
			Version: core.GetVersion(),
			Author:  core.AppAuthor,
			IsDirty: core.GetIsDirty(),
		})
	}
}

func (dash *Dashboard) aggregateHealthHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		proxies := dash.pm.GetProxies()
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

		dash.HTTP.JSONResponse(w, r, result)
	}
}

func (dash *Dashboard) toAPIProxy(name string, p *proxymanager.Proxy) apiProxy {
	status := p.GetStatus()

	url := p.GetURL()
	if status == model.ProxyStatusAuthenticating {
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
		Ports:         dash.toAPIPorts(p),
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

func (dash *Dashboard) toAPIPorts(p *proxymanager.Proxy) []apiPort {
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

func (dash *Dashboard) testWebhookHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if len(config.Config.Webhooks) == 0 {
			dash.writeJSONError(w, "no webhooks configured", http.StatusBadRequest)
			return
		}

		running := model.ProxyStatusRunning
		stopped := model.ProxyStatusStopped

		sender := webhook.NewSender(dash.Log, config.Config.Webhooks)
		defer sender.Close()

		if err := sender.SendSync(webhook.WebhookEvent{
			ProxyName: "test-proxy",
			Status:    running.String(),
			OldStatus: stopped.String(),
			Timestamp: time.Now().UTC().Format(time.RFC3339),
			Message:   "Test webhook from TSDProxy",
		}); err != nil {
			dash.writeJSONError(w, "webhook test failed: "+err.Error(), http.StatusBadGateway)
			return
		}

		dash.HTTP.JSONResponse(w, r, map[string]string{
			"message": "test webhook sent",
		})
	}
}

func (dash *Dashboard) writeJSONError(w http.ResponseWriter, message string, code int) {
	dash.HTTP.JSONResponseCode(w, nil, apiErrorResponse{Message: message, Code: code}, code)
}
