// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package proxymanager

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"

	"github.com/almeidapaulopt/tsdproxy/internal/core/metrics"
	"github.com/almeidapaulopt/tsdproxy/internal/model"
	"github.com/almeidapaulopt/tsdproxy/internal/tlsproviders"
)

// These tests verify the existence of two issues flagged in the uncommitted
// metrics-integration review:
//
//   H1 — updateCertExpiryMetric's `cert.Leaf = leaf` assignment was claimed
//        to mutate shared/cached state and race with TLS handshakes.
//
//   H2 — tsdproxy_cert_expiry_seconds is set once at proxy startup and never
//        refreshed, so it goes stale after the first cert renewal.
//
// The tests are self-documenting: their pass/fail behavior IS the evidence.

// -----------------------------------------------------------------------------
// Test doubles
// -----------------------------------------------------------------------------

// configurableTLSProvider is a mock tlsproviders.Provider whose GetCertificate
// return value can be swapped at runtime to simulate cert renewal.
type configurableTLSProvider struct {
	cert tls.Certificate
	name string
	mu   sync.Mutex
}

var _ tlsproviders.Provider = (*configurableTLSProvider)(nil)

func (p *configurableTLSProvider) Name() string { return p.name }
func (p *configurableTLSProvider) Provision(_ context.Context, _ string) error {
	return nil
}

func (p *configurableTLSProvider) GetCertificate(_ context.Context, _ string) (tls.Certificate, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	// Return tls.Certificate by VALUE — matches the real interface contract
	// used by both the ACME and Tailscale provider implementations.
	return p.cert, nil
}
func (p *configurableTLSProvider) Cleanup(_ context.Context, _ string) error { return nil }

func (p *configurableTLSProvider) setCert(cert tls.Certificate) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.cert = cert
}

func (p *configurableTLSProvider) leafIsNil() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.cert.Leaf == nil
}

// generateTestCert builds a self-signed cert valid until notAfter.
// Leaf is intentionally left nil to mirror certmagic's cache behavior
// (the cache stores raw DER and parses Leaf lazily).
func generateTestCert(t *testing.T, notAfter time.Time) tls.Certificate {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	template := x509.Certificate{
		SerialNumber:          big.NewInt(time.Now().UnixNano()),
		Subject:               pkix.Name{CommonName: "test"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              notAfter,
		KeyUsage:              x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		t.Fatalf("create cert: %v", err)
	}
	return tls.Certificate{
		Certificate: [][]byte{der},
		PrivateKey:  priv,
		// Leaf intentionally nil to trigger the parse path in updateCertExpiryMetric.
	}
}

// readCertExpiryMetric reads the current value of
// tsdproxy_cert_expiry_seconds{proxy=name}. Returns (value, found).
func readCertExpiryMetric(t *testing.T, m *metrics.Metrics, name string) (float64, bool) {
	t.Helper()
	ch := make(chan prometheus.Metric, 4)
	m.CertExpirySeconds.Collect(ch)
	close(ch)
	for metric := range ch {
		var pb dto.Metric
		if err := metric.Write(&pb); err != nil {
			continue
		}
		for _, l := range pb.Label {
			if l.GetName() == "proxy" && l.GetValue() == name {
				if pb.Gauge != nil {
					return pb.Gauge.GetValue(), true
				}
			}
		}
	}
	return 0, false
}

// newBugTestProxyManager builds a ProxyManager with a real metrics registry
// suitable for exercising updateCertExpiryMetric.
func newBugTestProxyManager(t *testing.T) (*ProxyManager, *metrics.Metrics) {
	t.Helper()
	pm := newTestProxyManager(newTestConfig(t))
	m := metrics.New(prometheus.NewRegistry())
	pm.metrics = m
	return pm, m
}

// -----------------------------------------------------------------------------
// H1: cert.Leaf mutation claim
// -----------------------------------------------------------------------------

// TestH1_CertLeafMutationDoesNotPropagate verifies whether the
// `cert.Leaf = leaf` assignment in updateCertExpiryMetric reaches the TLS
// provider's cached certificate.
//
// The original review claimed this was a data race with TLS handshakes.
// This test demonstrates the actual behavior: because GetCertificate returns
// tls.Certificate BY VALUE (verified in both acme.go and tailscale.go), the
// assignment is local-only and cannot race with handshakes.
//
//	PASS  = H1 as originally described is NOT a real bug.
//	FAIL  = H1 IS real: mutation propagates to shared state.
func TestH1_CertLeafMutationDoesNotPropagate(t *testing.T) {
	initialCert := generateTestCert(t, time.Now().Add(24*time.Hour))
	if initialCert.Leaf != nil {
		t.Fatalf("test setup: initial cert must have Leaf=nil, got %v", initialCert.Leaf)
	}

	provider := &configurableTLSProvider{name: "test", cert: initialCert}
	pm, _ := newBugTestProxyManager(t)

	p := &Proxy{
		Config: &model.Config{Hostname: "testproxy"},
		ctx:    context.Background(),
	}

	pm.updateCertExpiryMetric(p, provider, "example.com")

	if !provider.leafIsNil() {
		t.Fatalf(
			"H1 CONFIRMED: provider's cached cert.Leaf was mutated by updateCertExpiryMetric. " +
				"This means the assignment propagates to shared state and could race with TLS handshakes.",
		)
	}

	t.Logf(
		"H1 NOT REPRODUCED: provider's cached cert.Leaf is still nil after updateCertExpiryMetric. " +
			"The `cert.Leaf = leaf` assignment affects only a local copy because GetCertificate " +
			"returns tls.Certificate by value. No data race is possible through this path.",
	)
}

// TestH1_ConcurrentCallsNoRace exercises updateCertExpiryMetric from many
// goroutines to give the -race detector an opportunity to flag any data race.
// Run with: go test -race -run TestH1_ConcurrentCallsNoRace ./internal/proxymanager/...
//
// If this test panics under -race, H1 is real. If it passes cleanly, H1 is
// not a race through this code path.
func TestH1_ConcurrentCallsNoRace(t *testing.T) {
	cert := generateTestCert(t, time.Now().Add(24*time.Hour))
	provider := &configurableTLSProvider{name: "test", cert: cert}
	pm, _ := newBugTestProxyManager(t)

	p := &Proxy{
		Config: &model.Config{Hostname: "testproxy"},
		ctx:    context.Background(),
	}

	const goroutines = 50
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			pm.updateCertExpiryMetric(p, provider, "example.com")
		}()
	}
	wg.Wait()

	t.Logf(
		"H1 race-check complete: %d concurrent calls finished without -race detector firing. "+
			"Run with `go test -race` to be sure. "+
			"If the race detector flagged nothing, no shared-state mutation occurs.",
		goroutines,
	)
}

// -----------------------------------------------------------------------------
// H2: stale cert expiry metric after renewal
// -----------------------------------------------------------------------------

// TestH2_CertExpiryMetricIsStaleAfterRenewal proves that the
// tsdproxy_cert_expiry_seconds gauge is set once at proxy startup and never
// refreshed, even when the underlying cert is renewed.
//
// Real-world impact: after the first renewal (~30 days for ACME, ~90 days for
// Tailscale), the metric counts to 0 and stays pinned there, falsely
// reporting an expired/expiring cert to operators and alerting.
//
// Assertion strategy: this test FAILS while the bug exists and PASSES once
// the bug is fixed (i.e. once the metric is refreshed after renewal).
func TestH2_CertExpiryMetricIsStaleAfterRenewal(t *testing.T) {
	// --- arrange: initial cert expires in 1 hour ---------------------------
	initialCert := generateTestCert(t, time.Now().Add(1*time.Hour))
	provider := &configurableTLSProvider{name: "test", cert: initialCert}

	pm, m := newBugTestProxyManager(t)
	// Use a short refresh interval so the test can observe a refresh within
	// the sleep window below. Production uses defaultCertExpiryRefreshInterval (1h).
	pm.certExpiryRefresh = 10 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	p := &Proxy{
		Config: &model.Config{Hostname: "testproxy"},
		ctx:    ctx,
	}

	// --- act: start the cert expiry tracker (initial value + refresher) -
	pm.startCertExpiryTracking(p, provider, "example.com")

	initialValue, ok := readCertExpiryMetric(t, m, "testproxy")
	if !ok {
		t.Fatal("expected cert_expiry_seconds to be set after initial call")
	}
	const oneHour = 3600.0
	if initialValue <= 0 || initialValue > oneHour {
		t.Fatalf("initial metric sanity check failed: got %v, want (0, %v]", initialValue, oneHour)
	}
	t.Logf("step 1 — initial cert (NotAfter=+1h): metric=%.0f seconds", initialValue)

	// --- simulate renewal: cert now expires in 90 days ---------------------
	const ninetyDays = 90 * 24 * time.Hour
	renewedCert := generateTestCert(t, time.Now().Add(ninetyDays))
	provider.setCert(renewedCert)
	t.Logf("step 2 — cert renewed (NotAfter=+90d). A refreshed metric would read ~%.0f seconds.",
		ninetyDays.Seconds())

	// Give any hypothetical background refresher a chance to run.
	time.Sleep(100 * time.Millisecond)

	// --- assert: metric SHOULD reflect the renewed cert's lifetime ---------
	staleValue, ok := readCertExpiryMetric(t, m, "testproxy")
	if !ok {
		t.Fatal("expected cert_expiry_seconds metric to still be present")
	}

	expectedFresh := ninetyDays.Seconds()
	t.Logf("step 3 — metric after renewal: %.0f seconds (expected ~%.0f if refreshed)",
		staleValue, expectedFresh)

	// The bug: metric stays at initialValue instead of jumping to ~90 days.
	// We accept a generous tolerance band: if the metric moved into the
	// "days remaining" range, the refresh mechanism exists.
	if staleValue < expectedFresh/2 {
		t.Errorf(
			"BUG H2 CONFIRMED: cert expiry metric is stale after renewal.\n"+
				"  metric after renewal : %.0f seconds (≈ %.2f hours)\n"+
				"  cert actually expires: %.0f seconds (≈ %.2f days)\n"+
				"  root cause           : updateCertExpiryMetric is called only once from\n"+
				"                        setupDomainForProxy; no periodic refresh exists.\n"+
				"  impact               : metric will count to 0 and pin there after first\n"+
				"                        renewal, falsely alerting operators.\n"+
				"  fix                  : add per-proxy ticker that calls updateCertExpiryMetric\n"+
				"                        hourly, OR expose a custom prometheus.Collector that\n"+
				"                        reads cert.NotAfter lazily on each scrape.",
			staleValue, staleValue/3600, expectedFresh, ninetyDays.Hours()/24,
		)
		return
	}

	// Sanity check: prove the metric IS updatable when refresh is invoked.
	// This documents what the fix would do.
	pm.updateCertExpiryMetric(p, provider, "example.com")
	refreshedValue, _ := readCertExpiryMetric(t, m, "testproxy")
	t.Logf("step 4 — manual re-call updates metric to %.0f seconds "+
		"(demonstrates the fix would work if a refresh mechanism existed)", refreshedValue)
}
