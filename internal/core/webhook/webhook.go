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
	"time"

	"github.com/almeidapaulopt/tsdproxy/internal/config"
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
		event Event
		cfg   config.WebhookConfig
	}

	Sender struct {
		log     zerolog.Logger
		ctx     context.Context
		client  *http.Client
		cancel  context.CancelFunc
		queue   chan sendJob
		configs []config.WebhookConfig
		wg      sync.WaitGroup
		closeMu sync.Mutex
		closed  bool
	}
)

func NewSender(log zerolog.Logger, configs []config.WebhookConfig) *Sender {
	ctx, cancel := context.WithCancel(context.Background())
	s := &Sender{
		log:     log.With().Str("module", "webhook").Logger(),
		client:  &http.Client{Timeout: webhookTimeout},
		configs: configs,
		ctx:     ctx,
		cancel:  cancel,
		queue:   make(chan sendJob, webhookQueueSize),
	}
	s.wg.Add(webhookWorkers)
	for range webhookWorkers {
		go s.worker()
	}
	return s
}

func (s *Sender) Close() {
	s.closeMu.Lock()
	defer s.closeMu.Unlock()
	if s.closed {
		return
	}
	s.closed = true
	close(s.queue)

	s.wg.Wait()
	s.cancel()
}

func (s *Sender) worker() {
	defer s.wg.Done()
	for job := range s.queue {
		_ = s.sendWithRetry(job.cfg, job.event)
	}
}

func (s *Sender) Send(event Event) {
	s.closeMu.Lock()
	defer s.closeMu.Unlock()

	if s.closed {
		return
	}
	for _, cfg := range s.configs {
		if !eventMatchesFilter(event, cfg.Events) {
			continue
		}
		select {
		case s.queue <- sendJob{cfg: cfg, event: event}:
		default:
			s.log.Warn().Msg("webhook queue full, dropping event")
		}
	}
}

func (s *Sender) SendSync(event Event) error {
	var firstErr error
	for _, cfg := range s.configs {
		if !eventMatchesFilter(event, cfg.Events) {
			continue
		}
		if err := s.sendWithRetry(cfg, event); err != nil && firstErr == nil {
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

func (s *Sender) sendWithRetry(cfg config.WebhookConfig, event Event) error {
	safeURL := redactURL(cfg.URL)

	backoff := time.Second
	for attempt := 1; attempt <= maxRetries; attempt++ {
		select {
		case <-s.ctx.Done():
			return s.ctx.Err()
		default:
		}

		if err := s.sendOne(cfg, event); err != nil {
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

func (s *Sender) sendOne(cfg config.WebhookConfig, event Event) error {
	var body []byte
	var contentType string

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
	b, _ := json.Marshal(event)
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
	b, _ := json.Marshal(payload)
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
	b, _ := json.Marshal(payload)
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
