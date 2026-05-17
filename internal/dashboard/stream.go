// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package dashboard

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/almeidapaulopt/tsdproxy/internal/core"
	"github.com/almeidapaulopt/tsdproxy/internal/model"
	"github.com/almeidapaulopt/tsdproxy/internal/ui/pages"

	"github.com/a-h/templ"
	"github.com/google/uuid"
)

const (
	chanSizeSSEQueue = 64

	EventNotify EventType = iota
	EventScrollLogs
	EventTrimLogs
	EventUpdateSignals
	EventHTMXListRefresh
)

type (
	EventType int
	sseClient struct {
		channel chan SSEMessage
		done    chan struct{}
		userID  string
		search  string
	}

	SSEMessage struct {
		Comp    templ.Component
		Message string
		Type    EventType
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
		}

		dash.mtx.Lock()
		dash.sseClients[connID] = client
		dash.mtx.Unlock()

		dash.Log.Info().Msg("New client connected")
		defer dash.removeSSEClient(connID)

		go func() {
			dash.renderHTMXList(client)
			dash.updateUser(r, client)
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

				case EventUpdateSignals:
					err = SSEUpdateState(w, message.Message)

				case EventHTMXListRefresh:
					err = WriteSSEPartialComponent(w, "#proxy-list", "innerHTML", message.Comp)
				}
			}

			if err != nil {
				dash.Log.Error().Err(err).Msg("Error sending message to client")
				break LOOP
			}
		}
	}
}

func (dash *Dashboard) renderHTMXList(client *sseClient) {
	prefs := dash.loadPrefs(client.userID)
	proxies := dash.pm.GetProxies()
	view := BuildDashboardView(proxies, prefs, client.search)

	viewData := pages.DashboardData{
		Prefs: prefs,
	}
	viewData.Proxies = convertView(view)

	client.send(SSEMessage{
		Type: EventHTMXListRefresh,
		Comp: pages.ProxyListFragment(viewData),
	})
}

func (dash *Dashboard) updateUser(r *http.Request, client *sseClient) {
	who := core.ResolveWhois(r)

	signals := map[string]string{
		"user_username":      who.Username,
		"user_displayName":   who.DisplayName,
		"user_profilePicUrl": who.ProfilePicURL,
	}

	b, err := json.Marshal(signals)
	if err != nil {
		dash.Log.Error().Err(err).Msg("Error marshaling user signals")
		return
	}

	client.send(SSEMessage{
		Type:    EventUpdateSignals,
		Message: string(b),
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

func (dash *Dashboard) streamProxyUpdates() {
	for event := range dash.pm.SubscribeStatusEvents() {
		dash.mtx.RLock()
		for _, sseClient := range dash.sseClients {
			prefs := dash.loadPrefs(sseClient.userID)
			proxies := dash.pm.GetProxies()
			view := BuildDashboardView(proxies, prefs, sseClient.search)

			viewData := pages.DashboardData{
				Prefs: prefs,
			}
			viewData.Proxies = convertView(view)

			sseClient.send(SSEMessage{
				Type: EventHTMXListRefresh,
				Comp: pages.ProxyListFragment(viewData),
			})

			if event.Status == model.ProxyStatusStopped {
				sseClient.send(SSEMessage{
					Type:    EventNotify,
					Message: fmt.Sprintf("%s\x00Stopped", event.ID),
				})
			} else if event.Status == model.ProxyStatusError {
				sseClient.send(SSEMessage{
					Type:    EventNotify,
					Message: fmt.Sprintf("%s\x00Error", event.ID),
				})
			}
		}
		dash.mtx.RUnlock()
	}
}
