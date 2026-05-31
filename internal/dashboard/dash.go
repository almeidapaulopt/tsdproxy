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

	"github.com/almeidapaulopt/tsdproxy/internal/config"
	"github.com/almeidapaulopt/tsdproxy/internal/core"
	"github.com/almeidapaulopt/tsdproxy/internal/dom"
	"github.com/almeidapaulopt/tsdproxy/internal/model"
	"github.com/almeidapaulopt/tsdproxy/internal/proxymanager"
	"github.com/almeidapaulopt/tsdproxy/internal/ui"
	"github.com/almeidapaulopt/tsdproxy/internal/ui/pages"

	"github.com/a-h/templ"
	"github.com/rs/zerolog"
)

const (
	hxRequestHeader = "true"
	subjectRemote   = "__remote__"
)

type Dashboard struct {
	Log             zerolog.Logger
	HTTP            *core.HTTPServer
	pm              *proxymanager.ProxyManager
	prefs           *PreferencesStore
	sseClients      map[string]*sseClient
	stopCh          chan struct{}
	lastHealthState map[string]string
	mtx             sync.RWMutex
}

func NewDashboard(http *core.HTTPServer, log zerolog.Logger, pm *proxymanager.ProxyManager) *Dashboard {
	prefs, err := NewPreferencesStore(config.Config.Tailscale.DataDir, log)
	if err != nil {
		log.Error().Err(err).Msg("failed to initialize preferences store")
	}

	dash := &Dashboard{
		Log:             log.With().Str("module", "dashboard").Logger(),
		HTTP:            http,
		pm:              pm,
		prefs:           prefs,
		sseClients:      make(map[string]*sseClient),
		stopCh:          make(chan struct{}),
		lastHealthState: make(map[string]string),
	}

	go dash.streamProxyUpdates()

	return dash
}

func (dash *Dashboard) Close() {
	close(dash.stopCh)
}

// dashboardSubject returns the user identity key for preferences.
func (dash *Dashboard) dashboardSubject(r *http.Request) string {
	who := core.ResolveWhois(r)
	if who.ID != "" {
		return who.ID
	}
	if core.IsTrustedSource(r.RemoteAddr) {
		return "__localhost__"
	}
	if core.ValidAPIKey(r) {
		return "__apikey__"
	}
	return subjectRemote
}

func (dash *Dashboard) AddRoutes() {
	viewMW := core.ViewerMiddleware()
	adminMW := core.AdminMiddleware()

	dash.HTTP.Get("/{$}", viewMW(dash.dashboardHandler()))
	dash.HTTP.Get("/dashboard/list", viewMW(dash.listFragmentHandler()))
	dash.HTTP.Get("/dashboard/proxy/{name}/modal", viewMW(dash.proxyModalHandler()))
	dash.HTTP.Get("/stream", viewMW(dash.streamHandler()))

	dash.HTTP.Put("/api/dashboard/preferences", viewMW(dash.updatePreferencesHandler()))
	dash.HTTP.Post("/api/dashboard/pin/{name}", viewMW(dash.togglePinHandler()))

	dash.HTTP.Get("/stream/{name}/logs", adminMW(dash.streamProxyLogsHandler()))
	dash.HTTP.Post("/api/proxy/{name}/restart", adminMW(dash.restartHandler()))
	dash.HTTP.Post("/api/proxy/{name}/pause", adminMW(dash.pauseHandler()))
	dash.HTTP.Post("/api/proxy/{name}/resume", adminMW(dash.resumeHandler()))
	dash.HTTP.Post("/api/proxy/{name}/reauth", adminMW(dash.reauthHandler()))
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
		days := int(math.Round(d.Hours() / 24)) //nolint:mnd
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

func (dash *Dashboard) proxyActionHandler(action func(string) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		if name == "" {
			dash.writeJSONError(w, r, "invalid proxy name", http.StatusBadRequest)
			return
		}

		proxy, ok := dash.pm.GetProxy(name)
		if !ok || !proxy.Config.Dashboard.Visible {
			dash.writeJSONError(w, r, "proxy not found", http.StatusNotFound)
			return
		}

		if err := action(name); err != nil {
			dash.Log.Error().Err(err).Str("proxy", name).Msg("proxy action failed")
			dash.writeJSONError(w, r, "operation failed", http.StatusInternalServerError)
			return
		}

		if r.Header.Get("HX-Request") == hxRequestHeader {
			pinned := pinnedSet(dash.loadPrefs(dash.dashboardSubject(r)))
			_ = ui.RenderTempl(w, r, pages.ActionsPanel(buildProxyDataFromProxy(name, proxy, pinned, true)))
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

// reauthHandler triggers re-authentication by restarting the proxy.
// Tailscale re-auth requires a tsnet node restart, so this delegates to RestartProxy.
func (dash *Dashboard) reauthHandler() http.HandlerFunc {
	return dash.proxyActionHandler(dash.pm.RestartProxy)
}

func (dash *Dashboard) writeJSONError(w http.ResponseWriter, r *http.Request, message string, code int) {
	dash.HTTP.JSONResponseCode(w, r, map[string]any{"message": message, "code": code}, code)
}

func (dash *Dashboard) dashboardHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := dash.dashboardSubject(r)
		prefs := dash.loadPrefs(userID)
		who := core.ResolveWhois(r)

		viewData := dash.buildDashboardViewData(prefs, "", core.IsAdmin(r))
		viewData.User = who
		viewData.Version = core.GetVersion()

		if err := ui.RenderTempl(w, r, pages.Dashboard(viewData)); err != nil {
			dash.Log.Error().Err(err).Msg("failed to render template")
			return
		}
	}
}

func (dash *Dashboard) listFragmentHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := dash.dashboardSubject(r)
		prefs := dash.loadPrefs(userID)
		search := r.FormValue("search")
		connID := r.FormValue("sseConnId")

		dash.updateClientSearch(userID, connID, search)

		viewData := dash.buildDashboardViewData(prefs, search, core.IsAdmin(r))

		if err := ui.RenderTempl(w, r, pages.ProxyListFragment(viewData)); err != nil {
			dash.Log.Error().Err(err).Msg("failed to render template")
			return
		}
	}
}

func (dash *Dashboard) proxyModalHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		if name == "" {
			http.Error(w, "invalid proxy name", http.StatusBadRequest)
			return
		}

		proxy, ok := dash.pm.GetProxy(name)
		if !ok || !proxy.Config.Dashboard.Visible {
			http.Error(w, "proxy not found", http.StatusNotFound)
			return
		}

		data := buildProxyDataFromProxy(name, proxy, pinnedSet(dash.loadPrefs(dash.dashboardSubject(r))), core.IsAdmin(r))

		if err := ui.RenderTempl(w, r, pages.ProxyModal(data)); err != nil {
			dash.Log.Error().Err(err).Msg("failed to render template")
			return
		}
	}
}

func (dash *Dashboard) updatePreferencesHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := dash.dashboardSubject(r)

		if err := r.ParseForm(); err != nil {
			dash.writeJSONError(w, r, "invalid form data", http.StatusBadRequest)
			return
		}

		if userID == subjectRemote {
			dash.writeJSONError(w, r, "preferences require authentication", http.StatusForbidden)
			return
		}

		search := r.FormValue("search")

		if dash.prefs != nil {
			if err := dash.prefs.Update(userID, func(p *model.Preferences) {
				if v := r.FormValue("dark"); v != "" {
					p.Dark = v == "true"
				}
				if v := r.FormValue("view"); v != "" {
					p.View = v
				}
				if v := r.FormValue("sort"); v != "" {
					p.Sort = v
				}
				if v := r.FormValue("grouped"); v != "" {
					p.Grouped = v == "true"
				}
				if v := r.FormValue("filterStatus"); v != "" {
					p.FilterStatus = v
				}
				if v := r.FormValue("filterHealth"); v != "" {
					p.FilterHealth = v
				}
			}); err != nil {
				dash.Log.Error().Err(err).Msg("failed to save preferences")
			}
		}

		if r.FormValue("dark") != "" || r.FormValue("view") != "" {
			w.Header().Set("HX-Refresh", "true")
			w.WriteHeader(http.StatusOK)
			return
		}

		prefs := dash.loadPrefs(userID)
		viewData := dash.buildDashboardViewData(prefs, search, core.IsAdmin(r))

		if err := ui.RenderTempl(w, r, pages.ProxyListFragment(viewData)); err != nil {
			dash.Log.Error().Err(err).Msg("failed to render template")
			return
		}
	}
}

func (dash *Dashboard) togglePinHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		if name == "" {
			dash.writeJSONError(w, r, "invalid proxy name", http.StatusBadRequest)
			return
		}

		proxy, ok := dash.pm.GetProxy(name)
		if !ok || !proxy.Config.Dashboard.Visible {
			dash.writeJSONError(w, r, "proxy not found", http.StatusNotFound)
			return
		}

		userID := dash.dashboardSubject(r)
		if userID == subjectRemote {
			dash.writeJSONError(w, r, "preferences require authentication", http.StatusForbidden)
			return
		}
		if dash.prefs != nil {
			if _, err := dash.prefs.TogglePin(userID, name); err != nil {
				dash.Log.Error().Err(err).Msg("failed to toggle pin")
			}
		}

		prefs := dash.loadPrefs(userID)
		search := r.FormValue("search")
		viewData := dash.buildDashboardViewData(prefs, search, core.IsAdmin(r))

		if err := ui.RenderTempl(w, r, pages.ProxyListFragment(viewData)); err != nil {
			dash.Log.Error().Err(err).Msg("failed to render template")
			return
		}
	}
}

func (dash *Dashboard) loadPrefs(userID string) model.Preferences {
	if dash.prefs == nil {
		return defaultPreferences()
	}
	prefs, err := dash.prefs.Load(userID)
	if err != nil {
		dash.Log.Error().Err(err).Msg("failed to load preferences")
		return defaultPreferences()
	}
	return prefs
}

func (dash *Dashboard) buildDashboardViewData(prefs model.Preferences, search string, isAdmin bool) pages.DashboardData {
	proxies := dash.pm.GetProxies()
	return pages.DashboardData{
		Prefs:   prefs,
		Proxies: BuildDashboardView(proxies, prefs, search, isAdmin),
		IsAdmin: isAdmin,
	}
}

func buildProxyDataFromProxy(name string, p *proxymanager.Proxy, pinned map[string]bool, isAdmin bool) pages.ProxyData {
	status := p.GetStatus()
	proxyURL := p.GetURL()
	if status == model.ProxyStatusAuthenticating || status == model.ProxyStatusAwaitingApproval {
		proxyURL = p.GetAuthURL()
	}

	icon := p.Config.Dashboard.Icon
	if icon == "" {
		icon = model.DefaultDashboardIcon
	}

	label := p.Config.Dashboard.Label
	if label == "" {
		label = name
	}

	hostname := strings.TrimPrefix(proxyURL, "https://")
	hostname = strings.TrimPrefix(hostname, "http://")
	hostname = strings.TrimPrefix(hostname, "tcp://")
	hostname = strings.TrimPrefix(hostname, "udp://")

	ports := make([]pages.PortEntry, 0, len(p.Config.Ports))
	for _, target := range p.Config.Ports {
		scheme := target.ProxyProtocol
		portURL := scheme + "://" + hostname

		switch scheme {
		case model.ProtoHTTPS:
			if target.ProxyPort != 443 { //nolint:mnd
				portURL += ":" + strconv.Itoa(target.ProxyPort)
			}
		case model.ProtoHTTP:
			if target.ProxyPort != 80 { //nolint:mnd
				portURL += ":" + strconv.Itoa(target.ProxyPort)
			}
		default:
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
	if status == model.ProxyStatusAuthenticating || status == model.ProxyStatusAwaitingApproval {
		authURL = p.GetAuthURL()
	}

	enabled := status == model.ProxyStatusAuthenticating || status == model.ProxyStatusAwaitingApproval || status == model.ProxyStatusRunning

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

	return pages.ProxyData{
		Enabled:               enabled,
		Name:                  name,
		URL:                   proxyURL,
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
		Uptime:                formatDuration(p.GetUptime()),
		Pinned:                pinned[name],
		IsAdmin:               isAdmin,
		Domain:                p.Config.Domain,
		DNSStatus:             p.GetDNSStatus().String(),
		TLSStatus:             p.GetTLSStatus().String(),
	}
}

func (dash *Dashboard) validateLogStreamProxy(w http.ResponseWriter, r *http.Request) (string, *proxymanager.Proxy, bool) {
	name := r.PathValue("name")
	if name == "" {
		http.Error(w, "invalid proxy name", http.StatusBadRequest)
		return "", nil, false
	}

	proxy, ok := dash.pm.GetProxy(name)
	if !ok {
		http.Error(w, "proxy not found", http.StatusNotFound)
		return "", nil, false
	}

	if !proxy.Config.Dashboard.Visible {
		http.Error(w, "proxy not found", http.StatusNotFound)
		return "", nil, false
	}

	return name, proxy, true
}

func setupSSEHeaders(w http.ResponseWriter) bool {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	if _, ok := w.(http.Flusher); !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return false
	}
	return true
}

type logStreamer struct {
	w              http.ResponseWriter
	selector       string
	scrollSelector string
	trimSelector   string
	safeID         string
	maxLines       string
}

func newLogStreamer(w http.ResponseWriter, name string) *logStreamer {
	safeID := dom.SafeID(name)
	selector := "#log-lines-" + safeID
	return &logStreamer{
		w:              w,
		selector:       selector,
		scrollSelector: selector,
		trimSelector:   selector,
		safeID:         safeID,
		maxLines:       strconv.Itoa(proxymanager.DefaultLogBufferSize),
	}
}

func (s *logStreamer) writeAppend(cmp templ.Component) error {
	return WriteSSEPartialComponent(s.w, s.selector, "beforeend", cmp)
}

func (s *logStreamer) writeRemove(sel string) error {
	return WriteSSEPartialComponent(s.w, sel, "delete", nil)
}

func (s *logStreamer) writeClear() error {
	return WriteSSEPartialComponent(s.w, s.selector, "innerHTML", nil)
}

func (s *logStreamer) renderInitialSnapshot(snapshot []string) error {
	if err := s.writeClear(); err != nil {
		return err
	}
	if len(snapshot) > 0 {
		if err := s.writeAppend(pages.LogLines(snapshot)); err != nil {
			return err
		}
		return WriteSSE(s.w, "scroll-logs", s.scrollSelector)
	}
	return s.writeAppend(pages.LogPlaceholder(s.safeID))
}

func (s *logStreamer) streamEvents(done <-chan struct{}, ch <-chan string, snapshot []string) {
	placeholderRemoved := len(snapshot) > 0
	for {
		select {
		case <-done:
			return
		case line, ok := <-ch:
			if !ok {
				return
			}
			if !placeholderRemoved {
				if err := s.writeRemove("#log-placeholder-" + s.safeID); err != nil {
					return
				}
				placeholderRemoved = true
			}

			lines := s.drainBatch(ch, line)
			if err := s.writeAppend(pages.LogLines(lines)); err != nil {
				return
			}
			if err := WriteSSE(s.w, "trim-logs", s.trimSelector+"\n"+s.maxLines); err != nil {
				return
			}
			if err := WriteSSE(s.w, "scroll-logs", s.scrollSelector); err != nil {
				return
			}
		}
	}
}

func (s *logStreamer) drainBatch(ch <-chan string, first string) []string {
	const maxBatchSize = 50

	lines := []string{first}
	for len(lines) < maxBatchSize {
		select {
		case l, ok := <-ch:
			if !ok {
				return lines
			}
			lines = append(lines, l)
		default:
			return lines
		}
	}
	return lines
}

func (dash *Dashboard) streamProxyLogsHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name, proxy, ok := dash.validateLogStreamProxy(w, r)
		if !ok {
			return
		}

		snapshot, ch := proxy.SubscribeLogs()
		if ch == nil {
			http.Error(w, "no log buffer", http.StatusNotFound)
			return
		}
		defer proxy.UnsubscribeLogs(ch)

		if !setupSSEHeaders(w) {
			return
		}

		streamer := newLogStreamer(w, name)
		if err := streamer.renderInitialSnapshot(snapshot); err != nil {
			return
		}
		streamer.streamEvents(r.Context().Done(), ch, snapshot)
	}
}
