// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package tlsproviders

import (
	"context"
	"crypto/tls"
	"fmt"
	"sync"
)

type TLSLifecycleManager struct {
	states  map[string]TLSStatus
	mu      sync.RWMutex
	cleanup bool
}

func NewTLSLifecycleManager(cleanup bool) *TLSLifecycleManager {
	return &TLSLifecycleManager{
		states:  make(map[string]TLSStatus),
		cleanup: cleanup,
	}
}

func (lm *TLSLifecycleManager) Provision(ctx context.Context, provider Provider, domain string) error {
	lm.mu.Lock()
	lm.states[domain] = TLSStatusPending
	lm.mu.Unlock()

	if err := provider.Provision(ctx, domain); err != nil {
		lm.mu.Lock()
		lm.states[domain] = TLSStatusError
		lm.mu.Unlock()
		return fmt.Errorf("provision tls for %s: %w", domain, err)
	}

	lm.mu.Lock()
	lm.states[domain] = TLSStatusActive
	lm.mu.Unlock()
	return nil
}

func (lm *TLSLifecycleManager) GetCertificate(ctx context.Context, provider Provider, domain string) (tls.Certificate, error) {
	return provider.GetCertificate(ctx, domain)
}

func (lm *TLSLifecycleManager) Cleanup(ctx context.Context, provider Provider, domain string) error {
	if !lm.cleanup {
		return nil
	}

	if err := provider.Cleanup(ctx, domain); err != nil {
		return fmt.Errorf("cleanup tls for %s: %w", domain, err)
	}

	lm.mu.Lock()
	lm.states[domain] = TLSStatusNone
	lm.mu.Unlock()
	return nil
}

func (lm *TLSLifecycleManager) GetStatus(domain string) TLSStatus {
	lm.mu.RLock()
	defer lm.mu.RUnlock()
	return lm.states[domain]
}
