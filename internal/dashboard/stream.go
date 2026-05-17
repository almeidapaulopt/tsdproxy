// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package dashboard

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/almeidapaulopt/tsdproxy/internal/core"
	"github.com/almeidapaulopt/tsdproxy/internal/model"

	"github.com/a-h/templ"
	"github.com/google/uuid"
)

const (
	chanSizeSSEQueue = 64

	EventAppend EventType = iota
	EventMerge
	EventMergeMessage
	EventClearList
	EventRemoveElement
	EventSortList
	EventNotify
	EventScrollLogs
	EventTrimLogs
	EventUpdateSignals
)

// sseClient represents an SSE connection
type (
	EventType int
	sseClient struct {
		channel chan SSEMessage
		done    chan struct{}
	}

	SSEMessage struct {
		Comp    templ.Component
		Message string
		Type    EventType
	}
)

// send safely sends a message on the client channel.
// Returns false if the client is done (disconnected).
func (c *sseClient) send(msg SSEMessage) bool {
	select {
	case <-c.done:
		return false
	case c.channel <- msg:
		return true
	}
}

// Handler for the `/stream` endpoint
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

		// Create a new client
		client := &sseClient{
			channel: make(chan SSEMessage, chanSizeSSEQueue),
			done:    make(chan struct{}),
		}

		dash.mtx.Lock()
		dash.sseClients[connID] = client
		dash.mtx.Unlock()

		dash.Log.Info().Msg("New Client connected")
		// Ensure client is removed when disconnected
		defer dash.removeSSEClient(connID)

		go func() {
			dash.renderList(client)
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
				case EventAppend:
					err = SSEAppendHTML(w, message.Comp)

				case EventMerge:
					err = SSEMergeHTML(w, message.Comp)

				case EventMergeMessage:
					err = SSEMergeHTML(w, message.Message)

				case EventClearList:
					err = SSEClearList(w, message.Message)

				case EventRemoveElement:
					err = SSERemoveElement(w, message.Message)

				case EventSortList:
					err = WriteSSE(w, "sort-list", "")

				case EventNotify:
					err = WriteSSE(w, "notify", message.Message)

				case EventScrollLogs:
					err = WriteSSE(w, "scroll-logs", message.Message)

				case EventTrimLogs:
					err = WriteSSE(w, "trim-logs", message.Message)

				case EventUpdateSignals:
					err = SSEUpdateState(w, message.Message)
				}
			}

			if err != nil {
				dash.Log.Error().Err(err).Msg("Error sending message to client")
				break LOOP
			}
		}
	}
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
		dash.Log.Error().Err(err).Msg("Error marshalling user signals")
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
			switch event.Status {
			case model.ProxyStatusInitializing:
				dash.renderProxy(sseClient, event.ID, EventAppend)
				dash.streamSortList(sseClient)

			case model.ProxyStatusStopped:
				sseClient.send(SSEMessage{
					Type:    EventNotify,
					Message: fmt.Sprintf("%s\x00Stopped", event.ID),
				})
				sseClient.send(SSEMessage{
					Type:    EventRemoveElement,
					Message: "#" + event.ID,
				})

			default:
				dash.renderProxy(sseClient, event.ID, EventMerge)
				if event.Status == model.ProxyStatusError {
					sseClient.send(SSEMessage{
						Type:    EventNotify,
						Message: fmt.Sprintf("%s\x00Error", event.ID),
					})
				}
			}
		}
		dash.mtx.RUnlock()
	}
}

func (dash *Dashboard) streamSortList(client *sseClient) {
	client.send(SSEMessage{
		Type: EventSortList,
	})
}
