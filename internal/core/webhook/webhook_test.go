// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package webhook

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/almeidapaulopt/tsdproxy/internal/config"
	"github.com/almeidapaulopt/tsdproxy/internal/core/httpclient"
	"github.com/almeidapaulopt/tsdproxy/internal/model"

	"github.com/rs/zerolog"
)

func testEvent() Event {
	return Event{
		ProxyName: "test-proxy",
		Status:    "Running",
		OldStatus: "Stopped",
		Timestamp: "2026-01-01T00:00:00Z",
		Message:   "Proxy 'test-proxy' status changed from Stopped to Running",
	}
}

func TestNewEvent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		proxyName string
		oldStatus model.ProxyStatus
		newStatus model.ProxyStatus
	}{
		{
			name:      "stopped to running",
			proxyName: "myapp",
			oldStatus: model.ProxyStatusStopped,
			newStatus: model.ProxyStatusRunning,
		},
		{
			name:      "running to error",
			proxyName: "myapp",
			oldStatus: model.ProxyStatusRunning,
			newStatus: model.ProxyStatusError,
		},
		{
			name:      "initializing to authenticating",
			proxyName: "myapp",
			oldStatus: model.ProxyStatusInitializing,
			newStatus: model.ProxyStatusAuthenticating,
		},
		{
			name:      "running to stopped",
			proxyName: "myapp",
			oldStatus: model.ProxyStatusRunning,
			newStatus: model.ProxyStatusStopped,
		},
		{
			name:      "paused to running",
			proxyName: "myapp",
			oldStatus: model.ProxyStatusPaused,
			newStatus: model.ProxyStatusRunning,
		},
		{
			name:      "deviceconflict to reconciling",
			proxyName: "myapp",
			oldStatus: model.ProxyStatusDeviceConflict,
			newStatus: model.ProxyStatusReconciling,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			event := NewEvent(tt.proxyName, tt.oldStatus, tt.newStatus)

			if event.ProxyName != tt.proxyName {
				t.Errorf("ProxyName = %q, want %q", event.ProxyName, tt.proxyName)
			}
			if event.Status != tt.newStatus.String() {
				t.Errorf("Status = %q, want %q", event.Status, tt.newStatus.String())
			}
			if event.OldStatus != tt.oldStatus.String() {
				t.Errorf("OldStatus = %q, want %q", event.OldStatus, tt.oldStatus.String())
			}
			if event.Timestamp == "" {
				t.Error("Timestamp should not be empty")
			}

			wantMsg := fmt.Sprintf("Proxy '%s' status changed from %s to %s",
				tt.proxyName, tt.oldStatus.String(), tt.newStatus.String())
			if event.Message != wantMsg {
				t.Errorf("Message = %q, want %q", event.Message, wantMsg)
			}

			_, err := time.Parse(time.RFC3339, event.Timestamp)
			if err != nil {
				t.Errorf("Timestamp is not RFC3339: %v", err)
			}
		})
	}
}

func TestRedactURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		url  string
		want string
	}{
		{
			name: "valid URL with path and query",
			url:  "https://hooks.example.com/path?key=secret",
			want: "https://hooks.example.com",
		},
		{
			name: "valid URL with port",
			url:  "http://example.com:8080/webhook?id=123",
			want: "http://example.com:8080",
		},
		{
			name: "invalid URL",
			url:  "://invalid",
			want: "<invalid-url>",
		},
		{
			name: "empty string",
			url:  "",
			want: "://",
		},
		{
			name: "URL with fragment",
			url:  "https://hooks.example.com/secret#token",
			want: "https://hooks.example.com",
		},
		{ //nolint:gosec // G101: test fixture URL with credentials
			name: "URL with userinfo strips credentials",
			url:  "https://user:secret@hooks.example.com/webhook",
			want: "https://hooks.example.com",
		},
		{
			name: "URL with username only strips credentials",
			url:  "https://token@hooks.example.com/path",
			want: "https://hooks.example.com",
		},
		{ //nolint:gosec // G101: test fixture URL with credentials
			name: "URL with userinfo and port",
			url:  "http://user:pass@host.com:9090/hook",
			want: "http://host.com:9090",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := redactURL(tt.url); got != tt.want {
				t.Errorf("redactURL(%q) = %q, want %q", tt.url, got, tt.want)
			}
		})
	}
}

func TestSanitizeWebhookField(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "with mentions",
			input: "@user@example.com",
			want:  "@\u200buser@\u200bexample.com",
		},
		{
			name:  "no at signs",
			input: "plain-text",
			want:  "plain-text",
		},
		{
			name:  "empty string",
			input: "",
			want:  "",
		},
		{
			name:  "multiple consecutive at signs",
			input: "@@",
			want:  "@\u200b@\u200b",
		},
		{
			name:  "leading and trailing at signs",
			input: "@text@",
			want:  "@\u200btext@\u200b",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := sanitizeWebhookField(tt.input); got != tt.want {
				t.Errorf("sanitizeWebhookField(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestDiscordColor(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		status string
		want   int
	}{
		{name: "running", status: "Running", want: discordColorGreen},
		{name: "error", status: "Error", want: discordColorRed},
		{name: "stopped", status: "Stopped", want: discordColorRed},
		{name: "stopping", status: "Stopping", want: discordColorRed},
		{name: "authfailed", status: "AuthFailed", want: discordColorRed},
		{name: "authenticating", status: "Authenticating", want: discordColorOrange},
		{name: "awaitingapproval", status: "AwaitingApproval", want: discordColorOrange},
		{name: "paused", status: "Paused", want: discordColorOrange},
		{name: "deviceconflict", status: "DeviceConflict", want: discordColorYellow},
		{name: "reconciling", status: "Reconciling", want: discordColorBlue},
		{name: "initializing", status: "Initializing", want: discordColorBlue},
		{name: "starting", status: "Starting", want: discordColorBlue},
		{name: "default (unknown)", status: "Unknown", want: discordColorGrey},
		{name: "empty string", status: "", want: discordColorGrey},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := discordColor(tt.status); got != tt.want {
				t.Errorf("discordColor(%q) = %d, want %d", tt.status, got, tt.want)
			}
		})
	}
}

func TestEventMatchesFilter(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		event  Event
		filter []string
		want   bool
	}{
		{
			name:   "empty filter matches all",
			event:  Event{Status: "Running"},
			filter: nil,
			want:   true,
		},
		{
			name:   "empty filter slice matches all",
			event:  Event{Status: "Running"},
			filter: []string{},
			want:   true,
		},
		{
			name:   "matching filter",
			event:  Event{Status: "Running"},
			filter: []string{"Running"},
			want:   true,
		},
		{
			name:   "non-matching filter",
			event:  Event{Status: "Running"},
			filter: []string{"Stopped"},
			want:   false,
		},
		{
			name:   "case-insensitive match (lowercase filter)",
			event:  Event{Status: "Running"},
			filter: []string{statusRunning},
			want:   true,
		},
		{
			name:   "case-insensitive match (uppercase filter)",
			event:  Event{Status: statusError},
			filter: []string{"Error"},
			want:   true,
		},
		{
			name:   "multiple filters, one matches",
			event:  Event{Status: "Error"},
			filter: []string{"Running", "Stopped", "Error"},
			want:   true,
		},
		{
			name:   "multiple filters, none matches",
			event:  Event{Status: "Paused"},
			filter: []string{"Running", "Stopped", "Error"},
			want:   false,
		},
		{
			name:   "empty status matches empty filter entry",
			event:  Event{Status: ""},
			filter: []string{""},
			want:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := eventMatchesFilter(tt.event, tt.filter); got != tt.want {
				t.Errorf("eventMatchesFilter(%+v, %v) = %v, want %v",
					tt.event, tt.filter, got, tt.want)
			}
		})
	}
}

func TestFormatDiscord(t *testing.T) {
	t.Parallel()

	s := &Sender{log: zerolog.Nop()}
	event := Event{
		ProxyName: "test@proxy",
		Status:    "Running",
		OldStatus: "Stopped",
		Timestamp: "2026-01-01T00:00:00Z",
	}

	body, ct := s.formatDiscord(event)

	if ct != contentTypeJSON {
		t.Errorf("content type = %q, want %q", ct, contentTypeJSON)
	}

	validateDiscordPayload(t, body)
}

func validateDiscordPayload(t *testing.T, body []byte) {
	t.Helper()

	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("json.Unmarshal error: %v", err)
	}

	embeds, ok := payload["embeds"].([]any)
	if !ok {
		t.Fatal("payload['embeds'] is not an array")
	}
	if len(embeds) != 1 {
		t.Fatalf("expected 1 embed, got %d", len(embeds))
	}

	embed, ok := embeds[0].(map[string]any)
	if !ok {
		t.Fatal("embed is not a map")
	}

	validateDiscordEmbed(t, embed)
}

func validateDiscordEmbed(t *testing.T, embed map[string]any) {
	t.Helper()

	if title, _ := embed["title"].(string); title != "TSDProxy Status Update" {
		t.Errorf("title = %q, want %q", title, "TSDProxy Status Update")
	}
	if ts, _ := embed["timestamp"].(string); ts != "2026-01-01T00:00:00Z" {
		t.Errorf("timestamp = %q, want %q", ts, "2026-01-01T00:00:00Z")
	}
	if color, _ := embed["color"].(float64); int(color) != discordColorGreen {
		t.Errorf("color = %v, want %d", color, discordColorGreen)
	}

	fields, ok := embed["fields"].([]any)
	if !ok {
		t.Fatal("embed['fields'] is not an array")
	}
	if len(fields) != 3 {
		t.Fatalf("expected 3 fields, got %d", len(fields))
	}

	validateDiscordFields(t, fields)
}

func validateDiscordFields(t *testing.T, fields []any) {
	t.Helper()

	field0 := fields[0].(map[string]any)
	if v, _ := field0["value"].(string); !strings.Contains(v, "\u200b") {
		t.Errorf("proxy name should contain zero-width spaces, got %q", v)
	}

	fieldNames := []string{"Proxy", "Status", "Previous"}
	for i, field := range fields {
		f := field.(map[string]any)
		if name, _ := f["name"].(string); name != fieldNames[i] {
			t.Errorf("field %d name = %q, want %q", i, name, fieldNames[i])
		}
		if inline, ok := f["inline"]; ok {
			inl, ok := inline.(bool)
			if !ok || !inl {
				t.Errorf("field %d inline should be true, got %T(%v)", i, inline, inline)
			}
		}
	}

	field1 := fields[1].(map[string]any)
	if v, _ := field1["value"].(string); v != "Running" {
		t.Errorf("status value = %q, want %q", v, "Running")
	}
	field2 := fields[2].(map[string]any)
	if v, _ := field2["value"].(string); v != "Stopped" {
		t.Errorf("previous value = %q, want %q", v, "Stopped")
	}
}

func TestFormatSlack(t *testing.T) {
	t.Parallel()

	s := &Sender{log: zerolog.Nop()}
	event := Event{
		ProxyName: "test@proxy",
		Status:    "Running",
		OldStatus: "Stopped",
	}

	body, ct := s.formatSlack(event)

	if ct != "application/json" {
		t.Errorf("content type = %q, want %q", ct, "application/json")
	}

	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("json.Unmarshal error: %v", err)
	}

	text, _ := payload["text"].(string)
	if !strings.Contains(text, "\u200b") {
		t.Errorf("text should contain sanitized @ mentions, got %q", text)
	}
	if !strings.Contains(text, "Running") {
		t.Errorf("text should contain status, got %q", text)
	}

	blocks, ok := payload["blocks"].([]any)
	if !ok {
		t.Fatal("payload['blocks'] is not an array")
	}
	if len(blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(blocks))
	}

	block, ok := blocks[0].(map[string]any)
	if !ok {
		t.Fatal("block is not a map")
	}
	if block["type"] != "section" {
		t.Errorf("block type = %v, want 'section'", block["type"])
	}

	mdBlock, ok := block["text"].(map[string]any)
	if !ok {
		t.Fatal("block['text'] is not a map")
	}
	if mdBlock["type"] != "mrkdwn" {
		t.Errorf("text type = %v, want 'mrkdwn'", mdBlock["type"])
	}
	mdText, _ := mdBlock["text"].(string)
	if !strings.Contains(mdText, "TSDProxy Status Update") {
		t.Errorf("mrkdwn text should contain 'TSDProxy Status Update', got %q", mdText)
	}
	if !strings.Contains(mdText, "Running") || !strings.Contains(mdText, "Stopped") {
		t.Errorf("mrkdwn text should contain status values, got %q", mdText)
	}
	if !strings.Contains(mdText, "\u200b") {
		t.Errorf("mrkdwn text should contain sanitized @ mentions, got %q", mdText)
	}
}

func TestFormatNtfy(t *testing.T) {
	t.Parallel()

	s := &Sender{log: zerolog.Nop()}
	event := Event{
		ProxyName: "test@proxy",
		Status:    "Running",
		OldStatus: "Stopped",
	}

	body, ct := s.formatNtfy(event)

	if ct != contentTypeTextPlain {
		t.Errorf("content type = %q, want %q", ct, contentTypeTextPlain)
	}

	want := fmt.Sprintf("Proxy: %s\nStatus: %s\nPrevious: %s",
		sanitizeWebhookField("test@proxy"), "Running", "Stopped")
	if string(body) != want {
		t.Errorf("body = %q, want %q", string(body), want)
	}
}

func TestFormatGeneric(t *testing.T) {
	t.Parallel()

	s := &Sender{log: zerolog.Nop()}
	event := Event{
		ProxyName: "test-proxy",
		Status:    "Running",
		OldStatus: "Stopped",
		Timestamp: "2026-01-01T00:00:00Z",
		Message:   "test message",
	}

	body, ct := s.formatGeneric(event)

	if ct != "application/json" {
		t.Errorf("content type = %q, want %q", ct, "application/json")
	}

	var decoded Event
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("json.Unmarshal error: %v", err)
	}

	if decoded != event {
		t.Errorf("decoded event = %+v, want %+v", decoded, event)
	}
}

func TestSendOne(t *testing.T) {
	t.Parallel()

	event := testEvent()

	t.Run("generic type", func(t *testing.T) { t.Parallel(); testSendOneGeneric(t, event) })
	t.Run("discord type", func(t *testing.T) { t.Parallel(); testSendOneDiscord(t, event) })
	t.Run("slack type", func(t *testing.T) { t.Parallel(); testSendOneSlack(t, event) })
	t.Run("ntfy type", func(t *testing.T) { t.Parallel(); testSendOneNtfy(t, event) })
	t.Run("type casing is case-insensitive", func(t *testing.T) {
		t.Parallel()
		testSendOneCasing(t, event)
	})
	t.Run("error on status >= 300", func(t *testing.T) {
		t.Parallel()
		testSendOneErrorStatus(t, event)
	})
}

func testSendOneGeneric(t *testing.T, event Event) {
	t.Helper()

	var (
		contentType  string
		customHeader string
		reqMethod    string
		bodyBytes    []byte
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		contentType = r.Header.Get("Content-Type")
		customHeader = r.Header.Get("X-Test-Header")
		reqMethod = r.Method
		bodyBytes, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	s := &Sender{
		log:    zerolog.Nop(),
		client: &http.Client{Timeout: webhookTimeout},
		ctx:    context.Background(),
	}

	cfg := config.WebhookConfig{
		URL:     server.URL,
		Type:    "generic",
		Headers: map[string]string{"X-Test-Header": "test-value"},
	}

	if err := s.sendOne(cfg, event); err != nil {
		t.Fatalf("sendOne error: %v", err)
	}
	if reqMethod != http.MethodPost {
		t.Errorf("method = %q, want %q", reqMethod, http.MethodPost)
	}
	if contentType != contentTypeJSON {
		t.Errorf("Content-Type = %q, want %q", contentType, contentTypeJSON)
	}
	if customHeader != "test-value" {
		t.Errorf("X-Test-Header = %q, want %q", customHeader, "test-value")
	}

	var decoded Event
	if err := json.Unmarshal(bodyBytes, &decoded); err != nil {
		t.Fatalf("json.Unmarshal error: %v", err)
	}
	if decoded.ProxyName != event.ProxyName {
		t.Errorf("proxy name = %q, want %q", decoded.ProxyName, event.ProxyName)
	}
}

func testSendOneDiscord(t *testing.T, event Event) {
	t.Helper()

	var contentType string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		contentType = r.Header.Get("Content-Type")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	s := &Sender{
		log:    zerolog.Nop(),
		client: &http.Client{Timeout: webhookTimeout},
		ctx:    context.Background(),
	}

	if err := s.sendOne(config.WebhookConfig{URL: server.URL, Type: providerDiscord}, event); err != nil {
		t.Fatalf("sendOne error: %v", err)
	}
	if contentType != contentTypeJSON {
		t.Errorf("Content-Type = %q, want %q", contentType, contentTypeJSON)
	}
}

func testSendOneSlack(t *testing.T, event Event) {
	t.Helper()

	var contentType string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		contentType = r.Header.Get("Content-Type")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	s := &Sender{
		log:    zerolog.Nop(),
		client: &http.Client{Timeout: webhookTimeout},
		ctx:    context.Background(),
	}

	if err := s.sendOne(config.WebhookConfig{URL: server.URL, Type: "slack"}, event); err != nil {
		t.Fatalf("sendOne error: %v", err)
	}
	if contentType != contentTypeJSON {
		t.Errorf("Content-Type = %q, want %q", contentType, contentTypeJSON)
	}
}

func testSendOneNtfy(t *testing.T, event Event) {
	t.Helper()

	var (
		contentType string
		bodyBytes   []byte
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		contentType = r.Header.Get("Content-Type")
		bodyBytes, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	s := &Sender{
		log:    zerolog.Nop(),
		client: &http.Client{Timeout: webhookTimeout},
		ctx:    context.Background(),
	}

	if err := s.sendOne(config.WebhookConfig{URL: server.URL, Type: "ntfy"}, event); err != nil {
		t.Fatalf("sendOne error: %v", err)
	}
	if contentType != contentTypeTextPlain {
		t.Errorf("Content-Type = %q, want %q", contentType, contentTypeTextPlain)
	}
	if !bytes.Contains(bodyBytes, []byte("Proxy: test-proxy")) {
		t.Errorf("body should contain proxy name, got %q", string(bodyBytes))
	}
}

func testSendOneCasing(t *testing.T, event Event) {
	t.Helper()

	var contentType string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		contentType = r.Header.Get("Content-Type")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	s := &Sender{
		log:    zerolog.Nop(),
		client: &http.Client{Timeout: webhookTimeout},
		ctx:    context.Background(),
	}

	if err := s.sendOne(config.WebhookConfig{URL: server.URL, Type: "DISCORD"}, event); err != nil {
		t.Fatalf("sendOne error: %v", err)
	}
	if contentType != contentTypeJSON {
		t.Errorf("Content-Type = %q, want %q for uppercase type", contentType, contentTypeJSON)
	}
}

func testSendOneErrorStatus(t *testing.T, event Event) {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("invalid payload"))
	}))
	defer server.Close()

	s := &Sender{
		log:    zerolog.Nop(),
		client: &http.Client{Timeout: webhookTimeout},
		ctx:    context.Background(),
	}

	err := s.sendOne(config.WebhookConfig{URL: server.URL, Type: "generic"}, event)
	if err == nil {
		t.Fatal("expected error for status 400")
	}
	if !strings.Contains(err.Error(), "400") {
		t.Errorf("error should contain status code, got: %v", err)
	}
	if !strings.Contains(err.Error(), "invalid payload") {
		t.Errorf("error should contain response body, got: %v", err)
	}
}

func TestSendWithRetry(t *testing.T) {
	t.Parallel()

	event := testEvent()

	t.Run("success on first attempt", func(t *testing.T) {
		t.Parallel()

		var callCount atomic.Int32
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			callCount.Add(1)
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		s := &Sender{
			log:    zerolog.Nop(),
			client: &http.Client{Timeout: webhookTimeout},
			ctx:    context.Background(),
		}

		if err := s.sendWithRetry(config.WebhookConfig{URL: server.URL, Type: "generic"}, event); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if c := callCount.Load(); c != 1 {
			t.Errorf("expected 1 call, got %d", c)
		}
	})

	t.Run("retry then success", func(t *testing.T) {
		t.Parallel()

		var callCount atomic.Int32
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			if callCount.Add(1) < 2 {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		s := &Sender{
			log:    zerolog.Nop(),
			client: &http.Client{Timeout: webhookTimeout},
			ctx:    context.Background(),
		}

		if err := s.sendWithRetry(config.WebhookConfig{URL: server.URL, Type: "generic"}, event); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if c := callCount.Load(); c != 2 {
			t.Errorf("expected 2 calls, got %d", c)
		}
	})

	t.Run("all retries fail", func(t *testing.T) {
		t.Parallel()

		var callCount atomic.Int32
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			callCount.Add(1)
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer server.Close()

		s := &Sender{
			log:    zerolog.Nop(),
			client: &http.Client{Timeout: webhookTimeout},
			ctx:    context.Background(),
		}

		err := s.sendWithRetry(config.WebhookConfig{URL: server.URL, Type: "generic"}, event)
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "failed after 3 retries") {
			t.Errorf("error should mention retry count, got: %v", err)
		}
		if c := callCount.Load(); c != maxRetries {
			t.Errorf("expected %d calls, got %d", maxRetries, c)
		}
	})

	t.Run("context canceled", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer server.Close()

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		s := &Sender{
			log:    zerolog.Nop(),
			client: &http.Client{Timeout: webhookTimeout},
			ctx:    ctx,
			cancel: cancel,
		}

		err := s.sendWithRetry(config.WebhookConfig{URL: server.URL, Type: "generic"}, event)
		if err == nil {
			t.Fatal("expected error from canceled context")
		}
	})
}

func TestSend(t *testing.T) {
	t.Parallel()

	event := Event{ProxyName: "test", Status: "Running"}

	t.Run("all matching configs enqueued", func(t *testing.T) {
		t.Parallel()

		var callCount atomic.Int32
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			callCount.Add(1)
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		configs := []config.WebhookConfig{
			{URL: server.URL, Type: "generic"},
			{URL: server.URL, Type: "generic"},
		}

		s := NewSender(zerolog.Nop(), configs)
		s.Start()
		s.Send(event)
		s.Close()

		if c := callCount.Load(); c != 2 {
			t.Errorf("expected 2 requests, got %d", c)
		}
	})

	t.Run("non-matching event is skipped", func(t *testing.T) {
		t.Parallel()

		var callCount atomic.Int32
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			callCount.Add(1)
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		configs := []config.WebhookConfig{
			{URL: server.URL, Type: "generic", Events: []string{"Stopped"}},
		}

		s := NewSender(zerolog.Nop(), configs)
		s.Start()
		s.Send(event)
		s.Close()

		if c := callCount.Load(); c != 0 {
			t.Errorf("expected 0 requests, got %d", c)
		}
	})

	t.Run("closed sender drops event silently", func(t *testing.T) {
		t.Parallel()

		var callCount atomic.Int32
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			callCount.Add(1)
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		s := NewSender(zerolog.Nop(), []config.WebhookConfig{
			{URL: server.URL, Type: "generic"},
		})
		s.Start()
		s.Close()

		s.Send(event)

		if c := callCount.Load(); c != 0 {
			t.Errorf("expected 0 requests, got %d", c)
		}
	})
}

func TestSendSync(t *testing.T) {
	t.Parallel()

	event := Event{ProxyName: "test", Status: "Running"}

	t.Run("all configs succeed", func(t *testing.T) {
		t.Parallel()

		var callCount atomic.Int32
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			callCount.Add(1)
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		s := NewSender(zerolog.Nop(), []config.WebhookConfig{
			{URL: server.URL, Type: "generic"},
			{URL: server.URL, Type: "generic"},
		})
		defer s.Close()

		if err := s.SendSync(event); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if c := callCount.Load(); c != 2 {
			t.Errorf("expected 2 requests, got %d", c)
		}
	})

	t.Run("non-matching event returns nil", func(t *testing.T) {
		t.Parallel()

		var callCount atomic.Int32
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			callCount.Add(1)
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		s := NewSender(zerolog.Nop(), []config.WebhookConfig{
			{URL: server.URL, Type: "generic", Events: []string{"Stopped"}},
		})
		defer s.Close()

		if err := s.SendSync(event); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if c := callCount.Load(); c != 0 {
			t.Errorf("expected 0 requests, got %d", c)
		}
	})

	t.Run("returns first error", func(t *testing.T) {
		t.Parallel()

		var callCount atomic.Int32
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			callCount.Add(1)
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer server.Close()

		s := NewSender(zerolog.Nop(), []config.WebhookConfig{
			{URL: server.URL, Type: "generic"},
		})
		defer s.Close()

		err := s.SendSync(event)
		if err == nil {
			t.Fatal("expected error")
		}
		if c := callCount.Load(); c != maxRetries {
			t.Errorf("expected %d calls (1 config * %d retries), got %d",
				maxRetries, maxRetries, c)
		}
	})

	t.Run("continues after error for remaining configs", func(t *testing.T) {
		t.Parallel()

		var callCount atomic.Int32
		successServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			callCount.Add(1)
			w.WriteHeader(http.StatusOK)
		}))
		defer successServer.Close()

		failServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			callCount.Add(1)
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer failServer.Close()

		s := NewSender(zerolog.Nop(), []config.WebhookConfig{
			{URL: failServer.URL, Type: "generic"},
			{URL: successServer.URL, Type: "generic"},
		})
		defer s.Close()

		err := s.SendSync(event)
		if err == nil {
			t.Fatal("expected first error")
		}
		if c := callCount.Load(); c != maxRetries+1 {
			t.Errorf("expected %d calls, got %d", maxRetries+1, c)
		}
	})
}

func TestNewSenderClose(t *testing.T) {
	t.Parallel()

	t.Run("create and close", func(t *testing.T) {
		t.Parallel()

		cfg := config.WebhookConfig{
			URL:  "http://example.com/webhook",
			Type: providerDiscord,
		}
		s := NewSender(zerolog.Nop(), []config.WebhookConfig{cfg})

		if s == nil {
			t.Fatal("NewSender returned nil")
		}
		if len(s.configs) != 1 {
			t.Errorf("expected 1 config, got %d", len(s.configs))
		}
		if s.closed {
			t.Error("sender should not be closed initially")
		}
		if cap(s.queue) != webhookQueueSize {
			t.Errorf("queue capacity = %d, want %d", cap(s.queue), webhookQueueSize)
		}

		s.Close()
		if !s.closed {
			t.Error("sender should be closed after Close()")
		}
	})

	t.Run("close drains queue", func(t *testing.T) {
		t.Parallel()

		var callCount atomic.Int32
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			callCount.Add(1)
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		s := NewSender(zerolog.Nop(), []config.WebhookConfig{
			{URL: server.URL, Type: "generic"},
		})
		s.Start()

		s.Send(Event{ProxyName: "test", Status: "Running"})
		s.Close()

		if c := callCount.Load(); c != 1 {
			t.Errorf("expected 1 request (drained), got %d", c)
		}
	})

	t.Run("double close does not panic", func(_ *testing.T) {
		s := NewSender(zerolog.Nop(), nil)
		s.Close()
		s.Close()
	})

	t.Run("concurrent close does not panic", func(t *testing.T) {
		t.Parallel()

		const goroutines = 32
		s := NewSender(zerolog.Nop(), nil)
		s.Start()

		var wg sync.WaitGroup
		wg.Add(goroutines)
		start := make(chan struct{})
		for range goroutines {
			go func() {
				defer wg.Done()
				<-start
				s.Close()
			}()
		}
		close(start)
		wg.Wait()
	})

	t.Run("no configs", func(t *testing.T) {
		t.Parallel()

		s := NewSender(zerolog.Nop(), nil)
		s.Start()
		if s == nil {
			t.Fatal("NewSender returned nil")
		}
		if len(s.configs) != 0 {
			t.Errorf("expected 0 configs, got %d", len(s.configs))
		}
		s.Close()
	})
}

type callMockDoer struct {
	fn func(req *http.Request) (*http.Response, error)
}

var _ httpclient.Doer = (*callMockDoer)(nil)

func (m *callMockDoer) Do(req *http.Request) (*http.Response, error) {
	return m.fn(req)
}

func staticMockDoer(resp *http.Response, err error) *callMockDoer {
	return &callMockDoer{fn: func(_ *http.Request) (*http.Response, error) {
		return resp, err
	}}
}

func TestSendOne_NetworkError(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	mock := &callMockDoer{fn: func(_ *http.Request) (*http.Response, error) {
		calls.Add(1)
		return nil, errors.New("connection refused")
	}}
	s := &Sender{
		log:    zerolog.Nop(),
		client: mock,
		ctx:    context.Background(),
	}

	err := s.sendOne(config.WebhookConfig{URL: "http://127.0.0.1:1/webhook", Type: "generic"}, testEvent())
	if err == nil {
		t.Fatal("expected error from network failure")
	}
	if !strings.Contains(err.Error(), "error sending webhook") {
		t.Errorf("error should mention sending, got: %v", err)
	}
	if !strings.Contains(err.Error(), "connection refused") {
		t.Errorf("error should wrap underlying error, got: %v", err)
	}

	if n := calls.Load(); n != 1 {
		t.Fatalf("expected 1 call, got %d", n)
	}
}

func TestSendWithRetry_NetworkError_ThenSuccess(t *testing.T) {
	t.Parallel()

	event := testEvent()
	var callCount int

	mock := &callMockDoer{
		fn: func(_ *http.Request) (*http.Response, error) {
			callCount++
			if callCount == 1 {
				return nil, context.DeadlineExceeded
			}
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(""))}, nil
		},
	}

	s := &Sender{
		log:    zerolog.Nop(),
		client: mock,
		ctx:    context.Background(),
	}

	cfg := config.WebhookConfig{URL: "http://example.com/webhook", Type: "generic"}

	err := s.sendWithRetry(cfg, event)
	if err != nil {
		t.Fatalf("expected success after retry, got: %v", err)
	}
	if callCount != 2 {
		t.Errorf("expected 2 calls (1 fail + 1 success), got %d", callCount)
	}
}

func TestSendOne_502GatewayError(t *testing.T) {
	t.Parallel()

	mock := staticMockDoer(&http.Response{
		StatusCode: http.StatusBadGateway,
		Body:       io.NopCloser(strings.NewReader("<html>bad gateway</html>")),
	}, nil)

	s := &Sender{
		log:    zerolog.Nop(),
		client: mock,
		ctx:    context.Background(),
	}

	err := s.sendOne(config.WebhookConfig{URL: "http://example.com/webhook", Type: "generic"}, testEvent())
	if err == nil {
		t.Fatal("expected error for 502 status")
	}
	if !strings.Contains(err.Error(), "502") {
		t.Errorf("error should contain status code 502, got: %v", err)
	}
	if !strings.Contains(err.Error(), "bad gateway") {
		t.Errorf("error should contain response body, got: %v", err)
	}
}

func TestSend_QueueFullDropsEvent(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cfg := config.WebhookConfig{URL: "http://localhost:9999", Type: "generic"}

	s := &Sender{
		log:     zerolog.Nop(),
		ctx:     ctx,
		cancel:  cancel,
		client:  &http.Client{Timeout: webhookTimeout},
		queue:   make(chan sendJob, 1),
		configs: []config.WebhookConfig{cfg},
	}
	defer close(s.queue)

	event := Event{ProxyName: "test", Status: "Running"}

	s.Send(event)
	s.Send(event)

	var drained int
	for {
		select {
		case <-s.queue:
			drained++
		default:
			if drained != 1 {
				t.Errorf("expected 1 queued job, got %d", drained)
			}
			return
		}
	}
}
