// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package cloudflare

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/almeidapaulopt/tsdproxy/internal/core/httpclient"
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

		if r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/zones") && !strings.Contains(r.URL.Path, "/dns_records") {
			_, _ = w.Write([]byte(`{"success":true,"result":[{"id":"zone123","name":"example.com"}]}`))
			return
		}
		if r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/dns_records") {
			_, _ = w.Write([]byte(`{"success":true,"result":[]}`))
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

func TestProvider_CreateRecord_AlreadyExists(t *testing.T) {
	p, _ := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/zones") && !strings.Contains(r.URL.Path, "/dns_records") {
			_, _ = w.Write([]byte(`{"success":true,"result":[{"id":"zone123","name":"example.com"}]}`))
			return
		}
		if r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/dns_records") {
			_, _ = w.Write([]byte(`{"success":true,"result":[{"id":"rec123","type":"CNAME","name":"app.example.com","content":"myapp.tailnet.ts.net"}]}`))
			return
		}
	})

	err := p.CreateRecord(context.Background(), "app.example.com", "CNAME", "myapp.tailnet.ts.net")
	require.NoError(t, err)
}

func TestProvider_CreateRecord_StaleRecord(t *testing.T) {
	var gotDelete, gotCreate bool

	p, _ := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/zones") && !strings.Contains(r.URL.Path, "/dns_records") {
			_, _ = w.Write([]byte(`{"success":true,"result":[{"id":"zone123","name":"example.com"}]}`))
			return
		}
		if r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/dns_records") {
			_, _ = w.Write([]byte(`{"success":true,"result":[{"id":"rec_old","type":"CNAME","name":"app.example.com","content":"old.tailnet.ts.net"}]}`))
			return
		}
		if r.Method == http.MethodDelete {
			gotDelete = true
			assert.Contains(t, r.URL.Path, "rec_old")
			_, _ = w.Write([]byte(`{"success":true,"result":{"id":"rec_old"}}`))
			return
		}
		if r.Method == http.MethodPost {
			gotCreate = true
			_, _ = w.Write([]byte(`{"success":true,"result":{"id":"rec_new"}}`))
			return
		}
	})

	err := p.CreateRecord(context.Background(), "app.example.com", "CNAME", "myapp.tailnet.ts.net")
	require.NoError(t, err)
	assert.True(t, gotDelete, "stale record should be deleted")
	assert.True(t, gotCreate, "new record should be created")
}

type mockHTTPDoer struct {
	resp *http.Response
	err  error
}

var _ httpclient.Doer = (*mockHTTPDoer)(nil)

func (m *mockHTTPDoer) Do(_ *http.Request) (*http.Response, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.resp, nil
}

func newProviderWithMock(mock httpclient.Doer) *Provider {
	p := New("test-token")
	p.client = mock
	p.apiBaseURL = "http://mock.local"
	return p
}

func TestProvider_CreateRecord_Timeout(t *testing.T) {
	mock := &mockHTTPDoer{err: context.DeadlineExceeded}
	p := newProviderWithMock(mock)

	err := p.CreateRecord(context.Background(), "app.example.com", "CNAME", "myapp.tailnet.ts.net")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "context deadline exceeded")
}

func TestProvider_CreateRecord_502BadGateway(t *testing.T) {
	mock := &mockHTTPDoer{
		resp: &http.Response{
			StatusCode: http.StatusBadGateway,
			Header:     http.Header{"Content-Type": []string{"text/html"}},
			Body:       io.NopCloser(strings.NewReader("<html>bad gateway</html>")),
		},
	}
	p := newProviderWithMock(mock)

	err := p.CreateRecord(context.Background(), "app.example.com", "CNAME", "myapp.tailnet.ts.net")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "502")
}

func TestProvider_CreateRecord_429RateLimit(t *testing.T) {
	mock := &mockHTTPDoer{
		resp: &http.Response{
			StatusCode: http.StatusTooManyRequests,
			Header:     http.Header{"Content-Type": []string{"text/plain"}},
			Body:       io.NopCloser(strings.NewReader("rate limit exceeded")),
		},
	}
	p := newProviderWithMock(mock)

	err := p.CreateRecord(context.Background(), "app.example.com", "CNAME", "myapp.tailnet.ts.net")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "429")
}
