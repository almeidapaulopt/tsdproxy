// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package dashboard

import (
	"fmt"
	"net/http"
	"sync"

	"github.com/almeidapaulopt/tsdproxy/internal/dom"
	"github.com/almeidapaulopt/tsdproxy/internal/model"
	"github.com/almeidapaulopt/tsdproxy/internal/proxymanager"
	"github.com/almeidapaulopt/tsdproxy/internal/ui/pages"

	"github.com/a-h/templ"
	"github.com/google/uuid"
)

const (
	chanSizeSSEQueue = 64

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
		channel chan SSEMessage
		done    chan struct{}
		userID  string
		connID  string
		search  string
		mtx     sync.Mutex
	}

	SSEMessage struct {
		Comp    templ.Component
		Message string
		Type    EventType
		Target  string
		Swap    string
	}
)

func (c *sseClient) send(msg SSEMessage) bool {
	select {
	case <-c.done:
		return false
	case c.channel <- msg:
		return true
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
			connID:  connID,
		}

		dash.mtx.Lock()
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

	view := BuildDashboardView(proxies, prefs, search)

	viewData := pages.DashboardData{
		Prefs: prefs,
	}
	viewData.Proxies = convertView(view)

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
	client *sseClient
	userID string
	search string
}

func (dash *Dashboard) snapshotClients() []clientInfo {
	dash.mtx.RLock()
	defer dash.mtx.RUnlock()

	snapshot := make([]clientInfo, 0, len(dash.sseClients))
	for _, c := range dash.sseClients {
		c.mtx.Lock()
		search := c.search
		c.mtx.Unlock()
		snapshot = append(snapshot, clientInfo{client: c, userID: c.userID, search: search})
	}
	return snapshot
}

func (dash *Dashboard) streamProxyUpdates() {
	for event := range dash.pm.SubscribeStatusEvents() {
		clients := dash.snapshotClients()
		if len(clients) == 0 {
			continue
		}

		needsFull := dash.needsFullRender(clients, event)

		if needsFull {
			proxies := dash.pm.GetProxies()
			for _, ci := range clients {
				dash.renderHTMXList(ci.client, proxies)
				dash.sendStatusNotification(ci.client, event)
			}
			continue
		}

		proxy, ok := dash.pm.GetProxy(event.ID)
		if !ok {
			proxies := dash.pm.GetProxies()
			for _, ci := range clients {
				dash.renderHTMXList(ci.client, proxies)
				dash.sendStatusNotification(ci.client, event)
			}
			continue
		}

		if !proxy.Config.Dashboard.Visible {
			continue
		}

		type cardSend struct {
			client *sseClient
			msgs   []SSEMessage
		}

		safeName := dom.SafeID(event.ID)
		actionsTarget := "#actions-panel-" + safeName

		var sends []cardSend
		for _, ci := range clients {
			prefs := dash.loadPrefs(ci.userID)

			item := ProxyViewItem{Name: event.ID, Category: proxy.Config.Dashboard.Category, Proxy: proxy}
			if !matchesFilter(item, prefs, ci.search) {
				continue
			}

			data := buildProxyDataFromProxy(event.ID, proxy)
			sends = append(sends, cardSend{
				client: ci.client,
				msgs: []SSEMessage{
					{
						Type:   EventHTMXCardUpdate,
						Comp:   pages.ProxyCard(data),
						Target: "#proxy-" + safeName,
						Swap:   "outerHTML",
					},
					{
						Type:   EventHTMXCardUpdate,
						Comp:   pages.ActionsPanel(data),
						Target: actionsTarget,
						Swap:   "outerHTML",
					},
					{
						Type:   EventHTMXCardUpdate,
						Comp:   pages.ModalStatusBadge(data),
						Target: "#modal-status-" + safeName,
						Swap:   "outerHTML",
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
}

func (dash *Dashboard) needsFullRender(clients []clientInfo, event model.ProxyEvent) bool {
	for _, ci := range clients {
		prefs := dash.loadPrefs(ci.userID)

		if prefs.Sort == "status" || prefs.Sort == "health" {
			return true
		}

		if prefs.FilterStatus != "all" {
			oldMatch := prefs.FilterStatus == event.OldStatus.String()
			newMatch := prefs.FilterStatus == event.Status.String()
			if oldMatch != newMatch {
				return true
			}
		}

		if prefs.Grouped {
			return true
		}
	}
	return false
}

func (dash *Dashboard) sendStatusNotification(client *sseClient, event model.ProxyEvent) {
	if event.Status == model.ProxyStatusStopped {
		client.send(SSEMessage{
			Type:    EventNotify,
			Message: fmt.Sprintf("%s\x00Stopped", event.ID),
		})
	} else if event.Status == model.ProxyStatusError {
		client.send(SSEMessage{
			Type:    EventNotify,
			Message: fmt.Sprintf("%s\x00Error", event.ID),
		})
	}
}
