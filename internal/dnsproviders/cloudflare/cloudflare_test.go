// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package cloudflare

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testServer(t *testing.T, handler http.HandlerFunc) (*Provider, *httptest.Server) {
	t.Helper()
	ts := httptest.NewServer(handler)
	t.Cleanup(ts.Close)

	p := New("test-token")
	p.client = ts.Client()
	p.apiBaseURL = ts.URL

	return p, ts
}

func TestNew(t *testing.T) {
	p := New("test-token")
	assert.NotNil(t, p)
}

func TestProvider_Name(t *testing.T) {
	p := New("test-token")
	assert.Equal(t, "cloudflare", p.Name())
}

func TestProvider_CreateRecord(t *testing.T) {
	p, _ := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/zones") {
			_, _ = w.Write([]byte(`{"success":true,"result":[{"id":"zone123","name":"example.com"}]}`))
			return
		}
		if r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/dns_records") {
			assert.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))

			var record cfDNSRecord
			require.NoError(t, json.NewDecoder(r.Body).Decode(&record))
			assert.Equal(t, "CNAME", record.Type)
			assert.Equal(t, "app.example.com", record.Name)
			assert.Equal(t, "myapp.tailnet.ts.net", record.Content)

			_, _ = w.Write([]byte(`{"success":true,"result":{"id":"rec123"}}`))
			return
		}
	})

	err := p.CreateRecord(context.Background(), "app.example.com", "CNAME", "myapp.tailnet.ts.net")
	require.NoError(t, err)
}

func TestProvider_DeleteRecord(t *testing.T) {
	p, _ := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if strings.Contains(r.URL.Path, "/zones") && !strings.Contains(r.URL.Path, "/dns_records") {
			_, _ = w.Write([]byte(`{"success":true,"result":[{"id":"zone123","name":"example.com"}]}`))
			return
		}
		if r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/dns_records") {
			_, _ = w.Write([]byte(`{"success":true,"result":[{"id":"rec123","type":"CNAME","name":"app.example.com","content":"myapp.tailnet.ts.net"}]}`))
			return
		}
		if r.Method == http.MethodDelete {
			_, _ = w.Write([]byte(`{"success":true,"result":{"id":"rec123"}}`))
			return
		}
	})

	err := p.DeleteRecord(context.Background(), "app.example.com", "CNAME")
	require.NoError(t, err)
}

func TestProvider_ValidateRecord_Found(t *testing.T) {
	p, _ := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if strings.Contains(r.URL.Path, "/zones") && !strings.Contains(r.URL.Path, "/dns_records") {
			_, _ = w.Write([]byte(`{"success":true,"result":[{"id":"zone123","name":"example.com"}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"success":true,"result":[{"id":"rec123","type":"CNAME","name":"app.example.com","content":"myapp.tailnet.ts.net"}]}`))
	})

	ok, err := p.ValidateRecord(context.Background(), "app.example.com", "CNAME", "myapp.tailnet.ts.net")
	require.NoError(t, err)
	assert.True(t, ok)
}

func TestProvider_ValidateRecord_NotFound(t *testing.T) {
	p, _ := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if strings.Contains(r.URL.Path, "/zones") && !strings.Contains(r.URL.Path, "/dns_records") {
			_, _ = w.Write([]byte(`{"success":true,"result":[{"id":"zone123","name":"example.com"}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"success":true,"result":[{"id":"rec123","type":"CNAME","name":"app.example.com","content":"other.target.net"}]}`))
	})

	ok, err := p.ValidateRecord(context.Background(), "app.example.com", "CNAME", "myapp.tailnet.ts.net")
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestProvider_CreateRecord_NoZone(t *testing.T) {
	p, _ := testServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"result":[]}`))
	})

	err := p.CreateRecord(context.Background(), "app.example.com", "CNAME", "target")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no cloudflare zone found")
}

func TestProvider_CreateRecord_ApiError(t *testing.T) {
	p, _ := testServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":false,"errors":[{"code":1000,"message":"Invalid token"}]}`))
	})

	err := p.CreateRecord(context.Background(), "app.example.com", "CNAME", "target")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Invalid token")
}
