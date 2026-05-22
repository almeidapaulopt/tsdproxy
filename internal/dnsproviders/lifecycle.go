// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package dnsproviders

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

type LifecycleManager struct {
	states  map[string]DNSStatus
	mu      sync.RWMutex
	cleanup bool
}

func NewLifecycleManager(cleanup bool) *LifecycleManager {
	return &LifecycleManager{
		states:  make(map[string]DNSStatus),
		cleanup: cleanup,
	}
}

func (lm *LifecycleManager) SetupDNS(ctx context.Context, provider Provider, domain, targetHostname string) error {
	lm.mu.Lock()
	lm.states[domain] = DNSStatusPending
	lm.mu.Unlock()

	if err := provider.CreateRecord(ctx, domain, "CNAME", targetHostname); err != nil {
		lm.mu.Lock()
		lm.states[domain] = DNSStatusError
		lm.mu.Unlock()
		return fmt.Errorf("create cname for %s: %w", domain, err)
	}

	if err := Retry(ctx, func() error {
		ok, err := provider.ValidateRecord(ctx, domain, "CNAME", targetHostname)
		if err != nil {
			return err
		}
		if !ok {
			return errors.New("cname not propagated yet")
		}
		return nil
	}, 10, 2*time.Second); err != nil { //nolint:mnd
		lm.mu.Lock()
		lm.states[domain] = DNSStatusError
		lm.mu.Unlock()
		return fmt.Errorf("cname propagation for %s: %w", domain, err)
	}

	lm.mu.Lock()
	lm.states[domain] = DNSStatusActive
	lm.mu.Unlock()
	return nil
}

func (lm *LifecycleManager) CleanupDNS(ctx context.Context, provider Provider, domain string) error {
	if !lm.cleanup {
		return nil
	}

	if err := provider.DeleteRecord(ctx, domain, "CNAME"); err != nil {
		return fmt.Errorf("delete cname for %s: %w", domain, err)
	}

	lm.mu.Lock()
	lm.states[domain] = DNSStatusNone
	lm.mu.Unlock()
	return nil
}

func (lm *LifecycleManager) GetStatus(domain string) DNSStatus {
	lm.mu.RLock()
	defer lm.mu.RUnlock()
	return lm.states[domain]
}
