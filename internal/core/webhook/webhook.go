// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package webhook

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"text/template"
	"time"

	"github.com/almeidapaulopt/tsdproxy/internal/config"
	"github.com/almeidapaulopt/tsdproxy/internal/core/httpclient"
	"github.com/almeidapaulopt/tsdproxy/internal/model"

	"github.com/rs/zerolog"
)

const (
	maxRetries       = 3
	webhookWorkers   = 8
	webhookQueueSize = 256

	contentTypeJSON      = "application/json"
	contentTypeTextPlain = "text/plain"
	providerDiscord      = "discord"
	statusRunning        = "running"
	statusError          = "error"
	fieldInline          = "inline"
	fieldText            = "text"
	fieldName            = "name"
	fieldValue           = "value"

	webhookTimeout     = 10 * time.Second
	webhookMaxBody     = 512
	webhookMaxStatus   = 300
	discordColorGreen  = 5763719
	discordColorRed    = 15548997
	discordColorGrey   = 5766978
	discordColorYellow = 16426522
	discordColorBlue   = 3447003
	discordColorOrange = 16098851
)

type (
	Event struct {
		ProxyName string `json:"proxy"`
		Status    string `json:"status"`
		OldStatus string `json:"previousStatus"`
		Timestamp string `json:"timestamp"`
		Message   string `json:"message"`
	}

	sendJob struct {
		index int
		event Event
		cfg   config.WebhookConfig
	}

	Sender struct {
		log       zerolog.Logger
		ctx       context.Context
		client    httpclient.Doer
		cancel    context.CancelFunc
		queue     chan sendJob
		configs   []config.WebhookConfig
		templates map[int]*template.Template
		wg        sync.WaitGroup
		closeMu   sync.Mutex
		closed    bool
		started   bool
	}
)

func NewSender(log zerolog.Logger, configs []config.WebhookConfig, doer ...httpclient.Doer) *Sender {
	var client httpclient.Doer
	if len(doer) > 0 && doer[0] != nil {
		client = doer[0]
	} else {
		client = &http.Client{Timeout: webhookTimeout}
	}
	logger := log.With().Str("module", "webhook").Logger()
	ctx, cancel := context.WithCancel(context.Background())
	s := &Sender{
		log:       logger,
		client:    client,
		configs:   configs,
		templates: parseWebhookTemplates(logger, configs),
		ctx:       ctx,
		cancel:    cancel,
		queue:     make(chan sendJob, webhookQueueSize),
	}
	return s
}

// Start spawns the worker goroutines that drain the send queue.
// It must be called after NewSender and before Send. Calling Start on a
// sync sender (created with NewSyncSender, which has no queue) is a no-op.
func (s *Sender) Start() {
	if s.queue == nil {
		return
	}
	s.wg.Add(webhookWorkers)
	for range webhookWorkers {
		go s.worker()
	}
	s.started = true
}

// NewSyncSender returns a Sender without spawning worker goroutines.
// It is intended for one-shot use with SendSync (e.g. the test-webhook
// endpoint) so the request handler does not pay the cost of creating
// and tearing down 8 idle workers. Close is a no-op for sync senders.
func NewSyncSender(log zerolog.Logger, configs []config.WebhookConfig, doer ...httpclient.Doer) *Sender {
	var client httpclient.Doer
	if len(doer) > 0 && doer[0] != nil {
		client = doer[0]
	} else {
		client = &http.Client{Timeout: webhookTimeout}
	}
	logger := log.With().Str("module", "webhook").Logger()
	return &Sender{
		log:       logger,
		client:    client,
		configs:   configs,
		templates: parseWebhookTemplates(logger, configs),
		ctx:       context.Background(),
	}
}

func parseWebhookTemplates(log zerolog.Logger, configs []config.WebhookConfig) map[int]*template.Template {
	templates := make(map[int]*template.Template)
	for i, cfg := range configs {
		if cfg.Template == "" {
			continue
		}
		tmpl, err := template.New(fmt.Sprintf("webhook-%d", i)).Parse(cfg.Template)
		if err != nil {
			log.Error().
				Err(err).
				Str("url", redactURL(cfg.URL)).
				Int("index", i).
				Msg("invalid webhook template, using default payload")
			continue
		}
		templates[i] = tmpl
	}
	return templates
}

func (s *Sender) Close() {
	s.closeMu.Lock()
	defer s.closeMu.Unlock()
	if s.closed {
		return
	}
	s.closed = true
	if s.queue != nil {
		close(s.queue)
		s.wg.Wait()
	}
	if s.cancel != nil {
		s.cancel()
	}
}

func (s *Sender) worker() {
	defer s.wg.Done()
	for job := range s.queue {
		if err := s.sendWithRetry(job.index, job.cfg, job.event); err != nil {
			s.log.Error().Err(err).Str("url", redactURL(job.cfg.URL)).Msg("webhook delivery failed")
		}
	}
}

func (s *Sender) Send(event Event) {
	s.closeMu.Lock()
	defer s.closeMu.Unlock()

	if s.closed {
		return
	}
	for i, cfg := range s.configs {
		if !eventMatchesFilter(event, cfg.Events) {
			continue
		}
		select {
		case s.queue <- sendJob{index: i, cfg: cfg, event: event}:
		default:
			s.log.Warn().Msg("webhook queue full, dropping event")
		}
	}
}

func (s *Sender) SendSync(event Event) error {
	var firstErr error
	for i, cfg := range s.configs {
		if !eventMatchesFilter(event, cfg.Events) {
			continue
		}
		if err := s.sendWithRetry(i, cfg, event); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func eventMatchesFilter(event Event, filter []string) bool {
	if len(filter) == 0 {
		return true
	}
	for _, f := range filter {
		if strings.EqualFold(f, event.Status) {
			return true
		}
	}
	return false
}

func (s *Sender) sendWithRetry(index int, cfg config.WebhookConfig, event Event) error {
	safeURL := redactURL(cfg.URL)

	backoff := time.Second
	for attempt := 1; attempt <= maxRetries; attempt++ {
		select {
		case <-s.ctx.Done():
			return s.ctx.Err()
		default:
		}

		if err := s.sendOne(index, cfg, event); err != nil {
			s.log.Warn().
				Err(err).
				Str("url", safeURL).
				Int("attempt", attempt).
				Msg("webhook send failed")
			if attempt < maxRetries {
				select {
				case <-s.ctx.Done():
					return s.ctx.Err()
				case <-time.After(backoff):
				}
				backoff *= 2
			}
			continue
		}
		s.log.Debug().
			Str("url", safeURL).
			Str("type", cfg.Type).
			Str("proxy", event.ProxyName).
			Msg("webhook sent")
		return nil
	}
	err := fmt.Errorf("webhook %s failed after %d retries", safeURL, maxRetries)
	s.log.Error().
		Str("url", safeURL).
		Str("proxy", event.ProxyName).
		Msg(err.Error())
	return err
}

func (s *Sender) sendOne(index int, cfg config.WebhookConfig, event Event) error {
	var body []byte
	var contentType string

	if tmpl := s.templates[index]; tmpl != nil {
		var rendered bytes.Buffer
		if err := tmpl.Execute(&rendered, event); err != nil {
			s.log.Error().
				Err(err).
				Str("url", redactURL(cfg.URL)).
				Int("index", index).
				Msg("failed to render webhook template")
			return fmt.Errorf("error rendering webhook template: %w", err)
		}
		body, contentType = rendered.Bytes(), contentTypeJSON
	} else {
		switch strings.ToLower(cfg.Type) {
		case providerDiscord:
			body, contentType = s.formatDiscord(event)
		case "slack":
			body, contentType = s.formatSlack(event)
		case "ntfy":
			body, contentType = s.formatNtfy(event)
		default:
			body, contentType = s.formatGeneric(event)
		}
	}

	req, err := http.NewRequestWithContext(s.ctx, http.MethodPost, cfg.URL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("error creating webhook request: %w", err)
	}

	req.Header.Set("Content-Type", contentType)
	for k, v := range cfg.Headers {
		req.Header.Set(k, v)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("error sending webhook: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= webhookMaxStatus {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, webhookMaxBody))
		return fmt.Errorf("webhook returned status %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
}

func (s *Sender) formatGeneric(event Event) ([]byte, string) {
	b, err := json.Marshal(event)
	if err != nil {
		s.log.Error().Err(err).Msg("failed to marshal webhook payload")
		return []byte(`{"error":"marshal failed"}`), contentTypeJSON
	}
	return b, contentTypeJSON
}

func sanitizeWebhookField(s string) string {
	return strings.ReplaceAll(s, "@", "@\u200b")
}

func (s *Sender) formatDiscord(event Event) ([]byte, string) {
	name := sanitizeWebhookField(event.ProxyName)
	color := discordColor(event.Status)
	payload := map[string]any{
		"embeds": []map[string]any{
			{
				"title": "TSDProxy Status Update",
				"fields": []map[string]any{
					{fieldName: "Proxy", fieldValue: name, fieldInline: true},
					{fieldName: "Status", fieldValue: event.Status, fieldInline: true},
					{fieldName: "Previous", fieldValue: event.OldStatus, fieldInline: true},
				},
				"timestamp": event.Timestamp,
				"color":     color,
			},
		},
	}
	b, err := json.Marshal(payload)
	if err != nil {
		s.log.Error().Err(err).Msg("failed to marshal webhook payload")
		return []byte(`{"error":"marshal failed"}`), contentTypeJSON
	}
	return b, contentTypeJSON
}

func discordColor(status string) int {
	switch strings.ToLower(status) {
	case statusRunning:
		return discordColorGreen
	case statusError, "stopped", "stopping", "authfailed":
		return discordColorRed
	case "authenticating", "awaitingapproval", "paused":
		return discordColorOrange
	case "deviceconflict":
		return discordColorYellow
	case "reconciling", "initializing", "starting":
		return discordColorBlue
	default:
		return discordColorGrey
	}
}

func (s *Sender) formatSlack(event Event) ([]byte, string) {
	name := sanitizeWebhookField(event.ProxyName)
	payload := map[string]any{
		fieldText: fmt.Sprintf("TSDProxy: Proxy `%s` status changed to `%s`", name, event.Status),
		"blocks": []map[string]any{
			{
				"type": "section",
				fieldText: map[string]string{
					"type": "mrkdwn",
					fieldText: fmt.Sprintf("*TSDProxy Status Update*\nProxy: `%s`\nStatus: `%s`\nPrevious: `%s`",
						name, event.Status, event.OldStatus),
				},
			},
		},
	}
	b, err := json.Marshal(payload)
	if err != nil {
		s.log.Error().Err(err).Msg("failed to marshal webhook payload")
		return []byte(`{"error":"marshal failed"}`), contentTypeJSON
	}
	return b, contentTypeJSON
}

func (s *Sender) formatNtfy(event Event) ([]byte, string) {
	name := sanitizeWebhookField(event.ProxyName)
	payload := fmt.Sprintf("Proxy: %s\nStatus: %s\nPrevious: %s",
		name, event.Status, event.OldStatus)
	return []byte(payload), contentTypeTextPlain
}

func NewEvent(proxyName string, oldStatus, newStatus model.ProxyStatus) Event {
	return Event{
		ProxyName: proxyName,
		Status:    newStatus.String(),
		OldStatus: oldStatus.String(),
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Message:   fmt.Sprintf("Proxy '%s' status changed from %s to %s", proxyName, oldStatus.String(), newStatus.String()),
	}
}

// redactURL strips the path and query from rawURL, returning only scheme://host.
// This prevents secrets embedded in webhook URLs (e.g. Discord/Slack tokens)
// from appearing in log output.
func redactURL(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "<invalid-url>"
	}
	return u.Scheme + "://" + u.Host
}
