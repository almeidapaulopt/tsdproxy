// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package dashboard

import (
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/almeidapaulopt/tsdproxy/internal/core"
	"github.com/almeidapaulopt/tsdproxy/internal/dom"
	"github.com/almeidapaulopt/tsdproxy/internal/model"
	"github.com/almeidapaulopt/tsdproxy/internal/proxymanager"
	"github.com/almeidapaulopt/tsdproxy/internal/ui/pages"

	"github.com/a-h/templ"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

const (
	// chanSizeSSEQueue is buffered per client. Sized to absorb burst status
	// events (each event queues up to 4 messages per client: card,
	// actions-panel, modal-badge, optional notify). 256 gives ~64-event
	// headroom which is well above observed burst rates.
	chanSizeSSEQueue = 256
	maxSSEClients    = 256

	healthRefreshInterval = 10 * time.Second
	// resyncInterval bounds dashboard staleness. Even when an SSE message
	// is dropped (slow consumer / GC pause / network blip), every client
	// is guaranteed a full re-render at most this often.
	resyncInterval = 30 * time.Second

	EventNotify EventType = iota
	EventScrollLogs
	EventTrimLogs
	EventHTMXListRefresh
	EventHTMXCardUpdate
	EventConnID
)

type (
	EventType int
	sseClient struct {
		log     zerolog.Logger
		channel chan SSEMessage
		done    chan struct{}
		userID  string
		connID  string
		search  string
		mtx     sync.Mutex
		isAdmin bool
	}

	SSEMessage struct {
		Comp    templ.Component
		Message string
		Target  string
		Swap    string
		Type    EventType
	}
)

func (c *sseClient) send(msg SSEMessage) bool {
	select {
	case <-c.done:
		return false
	case c.channel <- msg:
		return true
	default:
		c.log.Warn().Str("conn", c.connID).Stringer("type", msg.Type).Msg("SSE client buffer full, dropping message")
		return false
	}
}

func (t EventType) String() string {
	switch t {
	case EventNotify:
		return "notify"
	case EventScrollLogs:
		return "scroll-logs"
	case EventTrimLogs:
		return "trim-logs"
	case EventHTMXListRefresh:
		return "list-refresh"
	case EventHTMXCardUpdate:
		return "card-update"
	case EventConnID:
		return "conn-id"
	default:
		return "unknown"
	}
}

func (dash *Dashboard) streamHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sessionID := r.Header.Get("X-Session-ID")
		connID := sessionID + "_" + uuid.New().String()

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		if _, ok := w.(http.Flusher); !ok {
			http.Error(w, "streaming not supported", http.StatusInternalServerError)
			return
		}

		userID := dash.dashboardSubject(r)

		client := &sseClient{
			channel: make(chan SSEMessage, chanSizeSSEQueue),
			done:    make(chan struct{}),
			userID:  userID,
			isAdmin: core.IsAdmin(r, dash.cfg),
			connID:  connID,
			log:     dash.Log,
		}

		dash.mtx.Lock()
		if len(dash.sseClients) >= maxSSEClients {
			dash.mtx.Unlock()
			http.Error(w, "too many SSE connections", http.StatusServiceUnavailable)
			return
		}
		dash.sseClients[connID] = client
		dash.mtx.Unlock()

		dash.Log.Info().Msg("New client connected")
		defer dash.removeSSEClient(connID)

		go func() {
			dash.renderHTMXList(client, dash.pm.GetProxies())
			dash.sendConnID(client, connID)
		}()

		var err error

	LOOP:
		for {
			select {
			case <-r.Context().Done():
				break LOOP
			case message := <-client.channel:
				switch message.Type {
				case EventNotify:
					err = WriteSSE(w, "notify", message.Message)

				case EventScrollLogs:
					err = WriteSSE(w, "scroll-logs", message.Message)

				case EventTrimLogs:
					err = WriteSSE(w, "trim-logs", message.Message)

				case EventHTMXListRefresh:
					err = WriteSSEPartialComponent(w, "#proxy-list", "innerHTML", message.Comp)

				case EventHTMXCardUpdate:
					err = WriteSSEPartialComponent(w, message.Target, message.Swap, message.Comp)

				case EventConnID:
					err = WriteSSE(w, "conn-id", message.Message)
				}
			}

			if err != nil {
				dash.Log.Error().Err(err).Msg("Error sending message to client")
				break LOOP
			}
		}
	}
}

func (dash *Dashboard) renderHTMXList(client *sseClient, proxies proxymanager.ProxyList) {
	prefs := dash.loadPrefs(client.userID)
	client.mtx.Lock()
	search := client.search
	client.mtx.Unlock()

	viewData := pages.DashboardData{
		Prefs:   prefs,
		Proxies: BuildDashboardView(proxies, prefs, search, client.isAdmin),
		IsAdmin: client.isAdmin,
	}

	client.send(SSEMessage{
		Type: EventHTMXListRefresh,
		Comp: pages.ProxyListFragment(viewData),
	})
}

func (dash *Dashboard) sendConnID(client *sseClient, connID string) {
	client.send(SSEMessage{
		Type:    EventConnID,
		Message: connID,
	})
}

func (dash *Dashboard) removeSSEClient(name string) {
	dash.mtx.Lock()

	if client, ok := dash.sseClients[name]; ok {
		delete(dash.sseClients, name)
		close(client.done)
	}
	dash.mtx.Unlock()

	dash.Log.Info().Msg("Client disconnected")
}

// updateClientSearch updates the search term for SSE clients belonging to
// the given user. If connID is non-empty only the matching connection is
// updated; otherwise all connections for the user are updated (graceful
// fallback for requests that arrive before the client receives its conn-id).
func (dash *Dashboard) updateClientSearch(userID, connID, search string) {
	dash.mtx.RLock()
	defer dash.mtx.RUnlock()

	for _, client := range dash.sseClients {
		if client.userID != userID {
			continue
		}
		if connID != "" && client.connID != connID {
			continue
		}
		client.mtx.Lock()
		client.search = search
		client.mtx.Unlock()
	}
}

type clientInfo struct {
	client  *sseClient
	userID  string
	search  string
	isAdmin bool
}

// clientPrefs pairs a client snapshot with its loaded preferences.
type clientPrefs struct {
	info  clientInfo
	prefs model.Preferences
}

func (dash *Dashboard) snapshotClients() []clientInfo {
	dash.mtx.RLock()
	defer dash.mtx.RUnlock()

	snapshot := make([]clientInfo, 0, len(dash.sseClients))
	for _, c := range dash.sseClients {
		c.mtx.Lock()
		search := c.search
		c.mtx.Unlock()
		snapshot = append(snapshot, clientInfo{client: c, userID: c.userID, isAdmin: c.isAdmin, search: search})
	}
	return snapshot
}

// snapshotClientsWithPrefs loads prefs once per client so event handlers
// don't pay for loadPrefs twice.
func (dash *Dashboard) snapshotClientsWithPrefs() []clientPrefs {
	clients := dash.snapshotClients()
	out := make([]clientPrefs, len(clients))
	for i, ci := range clients {
		out[i] = clientPrefs{info: ci, prefs: dash.loadPrefs(ci.userID)}
	}
	return out
}

func (dash *Dashboard) streamProxyUpdates() {
	events, cancelEvents := dash.pm.SubscribeStatusEvents()
	defer cancelEvents()

	healthTicker := time.NewTicker(healthRefreshInterval)
	defer healthTicker.Stop()
	resyncTicker := time.NewTicker(resyncInterval)
	defer resyncTicker.Stop()

	for {
		select {
		case <-dash.stopCh:
			return
		case event, ok := <-events:
			if !ok {
				return
			}
			dash.handleStatusEvent(event)

		case <-healthTicker.C:
			dash.refreshClientCards()

		case <-resyncTicker.C:
			dash.resyncAllClients()
		}
	}
}

// resyncAllClients forces a full re-render for every connected client,
// bounding dashboard staleness regardless of dropped SSE messages.
func (dash *Dashboard) resyncAllClients() {
	clients := dash.snapshotClients()
	if len(clients) == 0 {
		return
	}
	proxies := dash.pm.GetProxies()
	for _, ci := range clients {
		dash.renderHTMXList(ci.client, proxies)
	}
}

func (dash *Dashboard) handleStatusEvent(event model.ProxyEvent) {
	allClients := dash.snapshotClientsWithPrefs()
	if len(allClients) == 0 {
		return
	}

	proxy, proxyExists := dash.pm.GetProxy(event.ID)

	// If proxy is known but invisible, it doesn't exist on the dashboard.
	if proxyExists && !proxy.Config.Dashboard.Visible {
		return
	}

	var proxies proxymanager.ProxyList // Lazy loaded when a client needs a full list render

	type cardSend struct {
		client *sseClient
		msgs   []SSEMessage
	}
	var sends []cardSend

	safeName := dom.SafeID(event.ID)
	actionsTarget := "#actions-panel-" + safeName

	for _, cp := range allClients {
		var needsFull bool

		// If proxy was deleted, we must do a full render to remove it from the list
		if !proxyExists {
			needsFull = true
		} else {
			needsFull = clientNeedsFullRender(cp, event)
			// New proxies broadcast ProxyStatusInitializing before the card
			// element exists in the DOM, so outerHTML would be a no-op.
			// Force a full list render to make new cards appear.
			if !needsFull && event.Status == model.ProxyStatusInitializing {
				needsFull = true
			}
		}

		if needsFull {
			if proxies == nil {
				proxies = dash.pm.GetProxies()
			}
			dash.renderHTMXList(cp.info.client, proxies)
			dash.sendStatusNotification(cp.info.client, event)
			continue
		}

		data := buildProxyDataFromProxy(event.ID, proxy, pinnedSet(cp.prefs), cp.info.isAdmin)
		if !matchesFilter(data, cp.prefs, cp.info.search) {
			continue
		}

		sends = append(sends, cardSend{
			client: cp.info.client,
			msgs: []SSEMessage{
				{
					Type:   EventHTMXCardUpdate,
					Comp:   pages.ProxyCard(data),
					Target: "#proxy-" + safeName,
					Swap:   swapOuterHTML,
				},
				{
					Type:   EventHTMXCardUpdate,
					Comp:   pages.ActionsPanel(data),
					Target: actionsTarget,
					Swap:   swapOuterHTML,
				},
				{
					Type:   EventHTMXCardUpdate,
					Comp:   pages.ModalStatusBadge(data),
					Target: "#modal-status-" + safeName,
					Swap:   swapOuterHTML,
				},
			},
		})
	}

	for _, s := range sends {
		for _, msg := range s.msgs {
			s.client.send(msg)
		}
		dash.sendStatusNotification(s.client, event)
	}
}

// refreshClientCards pushes updated cards to all connected SSE clients
// so that health changes (which happen independently of status events)
// are reflected in the dashboard.
func (dash *Dashboard) refreshClientCards() {
	clients := dash.snapshotClients()
	if len(clients) == 0 {
		return
	}

	proxies := dash.pm.GetProxies()

	newHealth := make(map[string]string, len(proxies))
	for name, proxy := range proxies {
		h := proxy.GetHealth()
		key := fmt.Sprintf("%s:%s:%d", h.Status.String(), h.Error, h.Latency.Milliseconds())
		newHealth[name] = key
	}

	dash.mtx.Lock()
	changed := false

	for name, key := range newHealth {
		if last, ok := dash.lastHealthState[name]; !ok || last != key {
			changed = true
			break
		}
	}

	if !changed {
		for name := range dash.lastHealthState {
			if _, ok := newHealth[name]; !ok {
				changed = true
				break
			}
		}
	}

	dash.lastHealthState = newHealth
	dash.mtx.Unlock()

	if !changed {
		return
	}

	for _, ci := range clients {
		dash.renderHTMXList(ci.client, proxies)
	}
}

func clientNeedsFullRender(cp clientPrefs, event model.ProxyEvent) bool {
	if cp.prefs.Sort == sortStatus || cp.prefs.Sort == sortHealth {
		return true
	}

	if cp.prefs.FilterStatus != filterAll {
		oldMatch := cp.prefs.FilterStatus == event.OldStatus.String()
		newMatch := cp.prefs.FilterStatus == event.Status.String()
		if oldMatch != newMatch {
			return true
		}
	}

	if cp.prefs.Grouped {
		return true
	}

	return false
}

func (dash *Dashboard) sendStatusNotification(client *sseClient, event model.ProxyEvent) {
	switch event.Status {
	case model.ProxyStatusStopped:
		client.send(SSEMessage{
			Type:    EventNotify,
			Message: event.ID + "\x00Stopped",
		})
	case model.ProxyStatusError:
		client.send(SSEMessage{
			Type:    EventNotify,
			Message: event.ID + "\x00Error",
		})
	}
}
