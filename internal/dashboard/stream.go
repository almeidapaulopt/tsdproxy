// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package dashboard

import (
	"encoding/json"
	"net/http"

	"github.com/almeidapaulopt/tsdproxy/internal/consts"
	"github.com/almeidapaulopt/tsdproxy/internal/model"

	"github.com/a-h/templ"
	datastar "github.com/starfederation/datastar/sdk/go"
)

const (
	chanSizeSSEQueue = 0

	EventAppend EventType = iota
	EventMerge
	EventMergeMessage
	EventRemoveMessage
	EventScript
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

		sse := datastar.NewSSE(w, r)

		// Create a new client
		client := &sseClient{
			channel: make(chan SSEMessage, chanSizeSSEQueue),
			done:    make(chan struct{}),
		}

		// Register client
		dash.mtx.Lock()
		dash.sseClients[sessionID] = client
		dash.mtx.Unlock()

		dash.Log.Info().Msg("New Client connected")
		// Ensure client is removed when disconnected
		defer dash.removeSSEClient(sessionID)

		go func() {
			dash.renderList(client)
			dash.updateUser(r, client)
		}()

		var err error

		// Send messages to the client
	LOOP:
		for {
			select {
			case <-r.Context().Done():
				break LOOP
			case message := <-client.channel:
				switch message.Type {
				case EventAppend:
					err = sse.MergeFragmentTempl(
						message.Comp,
						datastar.WithMergeMode(datastar.FragmentMergeModeAppend),
						datastar.WithSelector("#proxy-list"),
					)

				case EventMerge:
					err = sse.MergeFragmentTempl(
						message.Comp,
					)

				case EventMergeMessage:
					err = sse.MergeFragments(message.Message)

				case EventRemoveMessage:
					err = sse.RemoveFragments(message.Message)

				case EventScript:
					err = sse.ExecuteScript(message.Message)

				case EventUpdateSignals:
					err = sse.MergeSignals([]byte(message.Message))
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
	signals := map[string]string{
		"user_username":     r.Header.Get(consts.HeaderUsername),
		"user_displayName":  r.Header.Get(consts.HeaderDisplayName),
		"user_profilePicUrl": r.Header.Get(consts.HeaderProfilePicURL),
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
					Type:    EventRemoveMessage,
					Message: "#" + event.ID,
				})

			default:
				dash.renderProxy(sseClient, event.ID, EventMerge)
			}
		}
		dash.mtx.RUnlock()
	}
}

func (dash *Dashboard) streamSortList(client *sseClient) {
	client.send(SSEMessage{
		Type:    EventScript,
		Message: "sortList()",
	})
}

