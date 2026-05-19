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
	"sync/atomic"
	"time"

	"github.com/almeidapaulopt/tsdproxy/internal/config"
	"github.com/almeidapaulopt/tsdproxy/internal/model"

	"github.com/rs/zerolog"
)

const (
	maxRetries       = 3
	webhookWorkers   = 8
	webhookQueueSize = 256
)

type (
	WebhookEvent struct {
		ProxyName string `json:"proxy"`
		Status    string `json:"status"`
		OldStatus string `json:"previous_status"`
		Timestamp string `json:"timestamp"`
		Message   string `json:"message"`
	}

	sendJob struct {
		event WebhookEvent
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
		closed  atomic.Bool
	}
)

func NewSender(log zerolog.Logger, configs []config.WebhookConfig) *Sender {
	ctx, cancel := context.WithCancel(context.Background())
	s := &Sender{
		log:     log.With().Str("module", "webhook").Logger(),
		client:  &http.Client{Timeout: 10 * time.Second},
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
	s.closed.Store(true)
	close(s.queue)
	s.closeMu.Unlock()

	s.wg.Wait()
	s.cancel()
}

func (s *Sender) worker() {
	defer s.wg.Done()
	for job := range s.queue {
		s.sendWithRetry(job.cfg, job.event)
	}
}

func (s *Sender) Send(event WebhookEvent) {
	s.closeMu.Lock()
	defer s.closeMu.Unlock()

	if s.closed.Load() {
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

func (s *Sender) SendSync(event WebhookEvent) error {
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

func eventMatchesFilter(event WebhookEvent, filter []string) bool {
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

func (s *Sender) sendWithRetry(cfg config.WebhookConfig, event WebhookEvent) error {
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

func (s *Sender) sendOne(cfg config.WebhookConfig, event WebhookEvent) error {
	var body []byte
	var contentType string

	switch strings.ToLower(cfg.Type) {
	case "discord":
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

	if resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("webhook returned status %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
}

func (s *Sender) formatGeneric(event WebhookEvent) ([]byte, string) {
	b, _ := json.Marshal(event)
	return b, "application/json"
}

func sanitizeWebhookField(s string) string {
	return strings.ReplaceAll(s, "@", "@\u200b")
}

func (s *Sender) formatDiscord(event WebhookEvent) ([]byte, string) {
	name := sanitizeWebhookField(event.ProxyName)
	color := discordColor(event.Status)
	payload := map[string]any{
		"embeds": []map[string]any{
			{
				"title": "TSDProxy Status Update",
				"fields": []map[string]any{
					{"name": "Proxy", "value": name, "inline": true},
					{"name": "Status", "value": event.Status, "inline": true},
					{"name": "Previous", "value": event.OldStatus, "inline": true},
				},
				"timestamp": event.Timestamp,
				"color":     color,
			},
		},
	}
	b, _ := json.Marshal(payload)
	return b, "application/json"
}

func discordColor(status string) int {
	switch strings.ToLower(status) {
	case "running":
		return 5763719
	case "error", "stopped":
		return 15548997
	default:
		return 5766978
	}
}

func (s *Sender) formatSlack(event WebhookEvent) ([]byte, string) {
	name := sanitizeWebhookField(event.ProxyName)
	payload := map[string]any{
		"text": fmt.Sprintf("TSDProxy: Proxy `%s` status changed to `%s`", name, event.Status),
		"blocks": []map[string]any{
			{
				"type": "section",
				"text": map[string]string{
					"type": "mrkdwn",
					"text": fmt.Sprintf("*TSDProxy Status Update*\nProxy: `%s`\nStatus: `%s`\nPrevious: `%s`",
						name, event.Status, event.OldStatus),
				},
			},
		},
	}
	b, _ := json.Marshal(payload)
	return b, "application/json"
}

func (s *Sender) formatNtfy(event WebhookEvent) ([]byte, string) {
	name := sanitizeWebhookField(event.ProxyName)
	payload := fmt.Sprintf("Proxy: %s\nStatus: %s\nPrevious: %s",
		name, event.Status, event.OldStatus)
	return []byte(payload), "text/plain"
}

func NewEvent(proxyName string, oldStatus, newStatus model.ProxyStatus) WebhookEvent {
	return WebhookEvent{
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
