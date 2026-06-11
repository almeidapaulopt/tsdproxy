// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package tailscale

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"tailscale.com/client/local"
	"tailscale.com/tsnet"

	"golang.org/x/sync/semaphore"
)

var (
	_ certPairer   = (*local.Client)(nil)
	_ certDomainer = (*tsnet.Server)(nil)
)

type mockCertPairer struct {
	err     error
	certPEM []byte
	keyPEM  []byte
	calls   atomic.Int32
	callErr atomic.Int32
}

func (m *mockCertPairer) CertPair(_ context.Context, _ string) ([]byte, []byte, error) {
	m.calls.Add(1)
	if m.err != nil && int(m.callErr.Load()) > 0 {
		m.callErr.Add(-1)
		return nil, nil, m.err
	}
	return m.certPEM, m.keyPEM, m.err
}

type mockCertDomainer struct {
	domains []string
}

func (m *mockCertDomainer) CertDomains() []string {
	return m.domains
}

func generateTestCertPEM(t *testing.T) ([]byte, []byte) {
	t.Helper()

	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "test.example.ts.net"},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
	}
	derBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	require.NoError(t, err)

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: derBytes})

	keyBytes, err := x509.MarshalECPrivateKey(priv)
	require.NoError(t, err)
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyBytes})

	return certPEM, keyPEM
}

func TestAcquireCert_NilParams(t *testing.T) {
	t.Parallel()

	t.Run("nil client", func(t *testing.T) {
		t.Parallel()
		sem := semaphore.NewWeighted(1)
		assert.NotPanics(t, func() {
			acquireCert(context.Background(), nil, &mockCertDomainer{}, sem, zerolog.Nop())
		})
	})

	t.Run("nil server", func(t *testing.T) {
		t.Parallel()
		sem := semaphore.NewWeighted(1)
		assert.NotPanics(t, func() {
			acquireCert(context.Background(), &mockCertPairer{}, nil, sem, zerolog.Nop())
		})
	})

	t.Run("nil semaphore", func(t *testing.T) {
		t.Parallel()
		assert.NotPanics(t, func() {
			acquireCert(context.Background(), &mockCertPairer{}, &mockCertDomainer{}, nil, zerolog.Nop())
		})
	})
}

func TestAcquireCert_NoDomains(t *testing.T) {
	t.Parallel()

	mc := &mockCertPairer{}
	md := &mockCertDomainer{domains: []string{}}
	sem := semaphore.NewWeighted(1)

	acquireCert(context.Background(), mc, md, sem, zerolog.Nop())
	assert.Equal(t, int32(0), mc.calls.Load())
}

func TestAcquireCert_Success(t *testing.T) {
	t.Parallel()

	certPEM, keyPEM := generateTestCertPEM(t)
	mc := &mockCertPairer{certPEM: certPEM, keyPEM: keyPEM}
	md := &mockCertDomainer{domains: []string{"test.example.ts.net"}}
	sem := semaphore.NewWeighted(1)

	acquireCert(context.Background(), mc, md, sem, zerolog.Nop())
	assert.Equal(t, int32(1), mc.calls.Load())
}

func TestAcquireCertForDomain_NilParams(t *testing.T) {
	t.Parallel()

	t.Run("nil client", func(t *testing.T) {
		t.Parallel()
		sem := semaphore.NewWeighted(1)
		assert.NotPanics(t, func() {
			acquireCertForDomain(context.Background(), nil, "test.ts.net", sem, zerolog.Nop())
		})
	})

	t.Run("empty domain", func(t *testing.T) {
		t.Parallel()
		sem := semaphore.NewWeighted(1)
		assert.NotPanics(t, func() {
			acquireCertForDomain(context.Background(), &mockCertPairer{}, "", sem, zerolog.Nop())
		})
	})

	t.Run("nil semaphore", func(t *testing.T) {
		t.Parallel()
		assert.NotPanics(t, func() {
			acquireCertForDomain(context.Background(), &mockCertPairer{}, "test.ts.net", nil, zerolog.Nop())
		})
	})
}

func TestAcquireCertForDomain_Success(t *testing.T) {
	t.Parallel()

	certPEM, keyPEM := generateTestCertPEM(t)
	mc := &mockCertPairer{certPEM: certPEM, keyPEM: keyPEM}
	sem := semaphore.NewWeighted(1)

	acquireCertForDomain(context.Background(), mc, "test.ts.net", sem, zerolog.Nop())
	assert.Equal(t, int32(1), mc.calls.Load())
}

func TestAcquireCertForDomain_RetryOnError(t *testing.T) {
	t.Parallel()

	mc := &mockCertPairer{err: errors.New("transient error")}
	sem := semaphore.NewWeighted(1)

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	start := time.Now()
	acquireCertForDomain(ctx, mc, "test.ts.net", sem, zerolog.Nop())
	elapsed := time.Since(start)

	assert.Greater(t, mc.calls.Load(), int32(0))
	assert.Less(t, elapsed, 2*time.Second, "should exit promptly on context cancellation")
}

func TestAcquireCertForDomain_ContextCancelled(t *testing.T) {
	t.Parallel()

	mc := &mockCertPairer{err: errors.New("persistent error")}
	sem := semaphore.NewWeighted(1)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	acquireCertForDomain(ctx, mc, "test.ts.net", sem, zerolog.Nop())
	assert.Equal(t, int32(0), mc.calls.Load(), "pre-canceled context should not call CertPair")
}

func TestCertPairToTLSCertificate_Success(t *testing.T) {
	t.Parallel()

	certPEM, keyPEM := generateTestCertPEM(t)
	mc := &mockCertPairer{certPEM: certPEM, keyPEM: keyPEM}

	cert, err := CertPairToTLSCertificate(context.Background(), mc, "test.example.ts.net")
	require.NoError(t, err)
	require.NotNil(t, cert)
	assert.Equal(t, "test.example.ts.net", cert.Leaf.Subject.CommonName)
}

func TestCertPairToTLSCertificate_CertPairError(t *testing.T) {
	t.Parallel()

	mc := &mockCertPairer{err: errors.New("tailscale error")}

	cert, err := CertPairToTLSCertificate(context.Background(), mc, "test.ts.net")
	require.Error(t, err)
	assert.Nil(t, cert)
	assert.Contains(t, err.Error(), "tailscale CertPair")
	assert.Contains(t, err.Error(), "tailscale error")
}

func TestCertPairToTLSCertificate_InvalidPEM(t *testing.T) {
	t.Parallel()

	mc := &mockCertPairer{
		certPEM: []byte("not a valid PEM"),
		keyPEM:  []byte("not a valid PEM either"),
	}

	cert, err := CertPairToTLSCertificate(context.Background(), mc, "test.ts.net")
	require.Error(t, err)
	assert.Nil(t, cert)
	assert.Contains(t, err.Error(), "parse cert")
}

func TestAcquireCertForDomain_SemaphoreContention(t *testing.T) {
	t.Parallel()

	certPEM, keyPEM := generateTestCertPEM(t)
	mc := &mockCertPairer{certPEM: certPEM, keyPEM: keyPEM}
	sem := semaphore.NewWeighted(1)

	require.NoError(t, sem.Acquire(context.Background(), 1))

	done := make(chan struct{})
	go func() {
		acquireCertForDomain(context.Background(), mc, "test.ts.net", sem, zerolog.Nop())
		close(done)
	}()

	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, int32(0), mc.calls.Load(), "should wait for semaphore")

	sem.Release(1)

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("acquireCertForDomain didn't complete after semaphore release")
	}
	assert.Equal(t, int32(1), mc.calls.Load())
}

func TestCertPairer_InterfaceCompliance(t *testing.T) {
	t.Parallel()

	var _ certPairer = (*mockCertPairer)(nil)
	var _ certDomainer = (*mockCertDomainer)(nil)
}

func TestAcquireCertForDomain_RetryBackoff(t *testing.T) {
	t.Parallel()

	mc := &mockCertPairer{err: context.Canceled}
	sem := semaphore.NewWeighted(1)

	start := time.Now()
	acquireCertForDomain(context.Background(), mc, "test.ts.net", sem, zerolog.Nop())
	elapsed := time.Since(start)

	assert.Less(t, elapsed, 2*time.Second, "context.Canceled should exit immediately without retrying")
}

func TestAcquireCertForDomain_ExactRetryCount(t *testing.T) {
	t.Parallel()

	mc := &mockCertPairer{err: errors.New("persistent transient error")}
	sem := semaphore.NewWeighted(1)

	// Short context (100ms) — well under the 10s initial backoff, so only
	// the first attempt can run before ctx.Done() fires during backoff.
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	acquireCertForDomain(ctx, mc, "test.ts.net", sem, zerolog.Nop())

	// With 100ms context and 10s initial backoff, only 1 call fits.
	// The retry loop selects on ctx.Done() during backoff and exits.
	assert.Equal(t, int32(1), mc.calls.Load(),
		"should make exactly 1 call before context expires (10s backoff prevents second)")
}
