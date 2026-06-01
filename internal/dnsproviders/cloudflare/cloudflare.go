// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package cloudflare

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/libdns/libdns"
	"golang.org/x/time/rate"

	"github.com/almeidapaulopt/tsdproxy/internal/core/secretstring"
	"github.com/almeidapaulopt/tsdproxy/internal/dnsproviders"
)

const defaultAPIBaseURL = "https://api.cloudflare.com/client/v4"

// Provider implements dnsproviders.Provider for Cloudflare DNS.
type Provider struct {
	client     *http.Client
	limiter    *rate.Limiter
	zoneCache  sync.Map
	apiToken   secretstring.SecretString
	apiBaseURL string
}

var _ dnsproviders.Provider = (*Provider)(nil)

type cfResponse struct {
	ResultInfo *cfResultInfo   `json:"result_info,omitempty"` //nolint:tagliatelle // Cloudflare API uses snake_case
	Result     json.RawMessage `json:"result"`
	Errors     []cfError       `json:"errors"`
	Success    bool            `json:"success"`
}

type cfError struct {
	Message string `json:"message"`
	Code    int    `json:"code"`
}

type cfResultInfo struct {
	Page       int `json:"page"`
	Count      int `json:"count"`
	Total      int `json:"total"`
	PerPage    int `json:"per_page"`    //nolint:tagliatelle // Cloudflare API uses snake_case
	TotalPages int `json:"total_pages"` //nolint:tagliatelle // Cloudflare API uses snake_case
}

type cfZone struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type cfDNSRecord struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Name    string `json:"name"`
	Content string `json:"content"`
	Proxied bool   `json:"proxied"`
	TTL     int    `json:"ttl"`
}

func New(apiToken string) *Provider {
	return &Provider{
		apiToken:   secretstring.SecretString(apiToken),
		client:     &http.Client{Timeout: 30 * time.Second}, //nolint:mnd
		apiBaseURL: defaultAPIBaseURL,
		limiter:    rate.NewLimiter(5, 10), //nolint:mnd // 5 req/s, burst of 10
	}
}

func (p *Provider) Name() string {
	return "cloudflare"
}

func (p *Provider) CreateRecord(ctx context.Context, domain, recordType, value string) error {
	zoneID, err := p.resolveZoneID(ctx, domain)
	if err != nil {
		return fmt.Errorf("cloudflare: resolve zone: %w", err)
	}

	records, err := p.listRecords(ctx, zoneID, recordType, domain)
	if err != nil {
		return fmt.Errorf("cloudflare: list records: %w", err)
	}

	found := false
	for _, r := range records {
		if strings.EqualFold(r.Content, value) {
			found = true
			continue
		}
		if _, delErr := p.doRequest(ctx, http.MethodDelete,
			fmt.Sprintf("/zones/%s/dns_records/%s", zoneID, r.ID), nil); delErr != nil {
			return fmt.Errorf("cloudflare: delete stale record %s: %w", r.ID, delErr)
		}
	}

	if found {
		return nil
	}

	body := cfDNSRecord{
		Type:    recordType,
		Name:    domain,
		Content: value,
		Proxied: false,
		TTL:     1,
	}

	_, err = p.doRequest(ctx, http.MethodPost, fmt.Sprintf("/zones/%s/dns_records", zoneID), body)
	if err != nil {
		return fmt.Errorf("cloudflare: create %s record for %s: %w", recordType, domain, err)
	}
	return nil
}

func (p *Provider) DeleteRecord(ctx context.Context, domain, recordType string) error {
	zoneID, err := p.resolveZoneID(ctx, domain)
	if err != nil {
		return fmt.Errorf("cloudflare: resolve zone: %w", err)
	}

	records, err := p.listRecords(ctx, zoneID, recordType, domain)
	if err != nil {
		return fmt.Errorf("cloudflare: list records: %w", err)
	}

	for _, r := range records {
		if _, err := p.doRequest(ctx, http.MethodDelete, fmt.Sprintf("/zones/%s/dns_records/%s", zoneID, r.ID), nil); err != nil {
			return fmt.Errorf("cloudflare: delete record %s: %w", r.ID, err)
		}
	}
	return nil
}

func (p *Provider) ValidateRecord(ctx context.Context, domain, recordType, expectedValue string) (bool, error) {
	zoneID, err := p.resolveZoneID(ctx, domain)
	if err != nil {
		return false, fmt.Errorf("cloudflare: resolve zone: %w", err)
	}

	records, err := p.listRecords(ctx, zoneID, recordType, domain)
	if err != nil {
		return false, fmt.Errorf("cloudflare: list records: %w", err)
	}

	for _, r := range records {
		if strings.EqualFold(r.Content, expectedValue) {
			return true, nil
		}
	}
	return false, nil
}

func (p *Provider) resolveZoneID(ctx context.Context, domain string) (string, error) {
	parts := strings.Split(strings.TrimSuffix(domain, "."), ".")
	if len(parts) < 2 { //nolint:mnd
		return "", fmt.Errorf("invalid domain %q", domain)
	}

	// Try progressively shorter zone names from the full domain down to
	// the last two labels. This handles multi-part TLDs like co.uk, com.br
	// where taking only the last two labels would yield the wrong zone.
	for i := 0; i < len(parts)-1; i++ {
		zoneName := strings.Join(parts[i:], ".")

		if cached, ok := p.zoneCache.Load(zoneName); ok {
			return cached.(string), nil
		}

		resp, err := p.doRequest(ctx, http.MethodGet, "/zones?name="+url.QueryEscape(zoneName), nil)
		if err != nil {
			return "", err
		}

		var zones []cfZone
		if err := json.Unmarshal(resp, &zones); err != nil {
			return "", fmt.Errorf("parse zones response: %w", err)
		}

		if len(zones) > 0 {
			p.zoneCache.Store(zoneName, zones[0].ID)
			return zones[0].ID, nil
		}
	}

	return "", fmt.Errorf("no cloudflare zone found for %s", domain)
}

func (p *Provider) listRecords(ctx context.Context, zoneID, recordType, domain string) ([]cfDNSRecord, error) {
	path := fmt.Sprintf("/zones/%s/dns_records?type=%s&name=%s",
		url.PathEscape(zoneID), url.QueryEscape(recordType), url.QueryEscape(domain))
	resp, err := p.doRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}

	var records []cfDNSRecord
	if err := json.Unmarshal(resp, &records); err != nil {
		return nil, fmt.Errorf("parse records response: %w", err)
	}
	return records, nil
}

// AppendRecords implements libdns.RecordAppender for certmagic ACME DNS-01.
func (p *Provider) AppendRecords(ctx context.Context, zone string, recs []libdns.Record) ([]libdns.Record, error) {
	zoneID, err := p.resolveZoneID(ctx, zone)
	if err != nil {
		return nil, err
	}

	var created []libdns.Record
	for _, rec := range recs {
		rr := rec.RR()
		body := cfDNSRecord{
			Type:    rr.Type,
			Name:    rr.Name,
			Content: rr.Data,
			Proxied: false,
			TTL:     int(rr.TTL.Seconds()),
		}

		result, err := p.doRequest(ctx, http.MethodPost, fmt.Sprintf("/zones/%s/dns_records", zoneID), body)
		if err != nil {
			return created, fmt.Errorf("append record %s: %w", rr.Name, err)
		}

		var cfRec cfDNSRecord
		if err := json.Unmarshal(result, &cfRec); err == nil {
			created = append(created, rec)
		}
	}
	return created, nil
}

// DeleteRecords implements libdns.RecordDeleter for certmagic ACME DNS-01.
func (p *Provider) DeleteRecords(ctx context.Context, zone string, recs []libdns.Record) ([]libdns.Record, error) {
	zoneID, err := p.resolveZoneID(ctx, zone)
	if err != nil {
		return nil, err
	}

	var deleted []libdns.Record
	for _, rec := range recs {
		rr := rec.RR()
		records, err := p.listRecords(ctx, zoneID, rr.Type, rr.Name)
		if err != nil {
			continue
		}

		for _, r := range records {
			if _, err := p.doRequest(ctx, http.MethodDelete, fmt.Sprintf("/zones/%s/dns_records/%s", zoneID, r.ID), nil); err == nil {
				deleted = append(deleted, rec)
			}
		}
	}
	return deleted, nil
}

func (p *Provider) doRequest(ctx context.Context, method, path string, body any) (json.RawMessage, error) {
	// Rate limit to avoid hitting Cloudflare API limits.
	if err := p.limiter.Wait(ctx); err != nil {
		return nil, fmt.Errorf("rate limiter: %w", err)
	}

	var reqBody io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal request: %w", err)
		}
		reqBody = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, p.apiBaseURL+path, reqBody)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+p.apiToken.Value())
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	var cfResp cfResponse
	if err := json.Unmarshal(data, &cfResp); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	if !cfResp.Success {
		if len(cfResp.Errors) > 0 {
			return nil, fmt.Errorf("cloudflare api error: %s", cfResp.Errors[0].Message)
		}
		return nil, errors.New("cloudflare api returned success=false")
	}

	return cfResp.Result, nil
}
