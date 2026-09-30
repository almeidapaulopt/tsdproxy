// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package tlsproviders

import (
	"context"
	"fmt"

	"github.com/almeidapaulopt/tsdproxy/internal/lifecycle"
)

type LifecycleManager struct {
	tracker *lifecycle.StateTracker
	cleanup bool
}

func NewLifecycleManager(cleanup bool) *LifecycleManager {
	return &LifecycleManager{
		tracker: lifecycle.NewStateTracker(),
		cleanup: cleanup,
	}
}

func (lm *LifecycleManager) Provision(ctx context.Context, provider Provider, domain string) error {
	lm.tracker.Set(domain, TLSStatusPending)

	if err := provider.Provision(ctx, domain); err != nil {
		lm.tracker.Set(domain, TLSStatusError)
		return fmt.Errorf("provision tls for %s: %w", domain, err)
	}

	lm.tracker.Set(domain, TLSStatusActive)
	return nil
}

func (lm *LifecycleManager) Cleanup(ctx context.Context, provider Provider, domain string) error {
	if !lm.cleanup {
		return nil
	}

	if err := provider.Cleanup(ctx, domain); err != nil {
		return fmt.Errorf("cleanup tls for %s: %w", domain, err)
	}

	lm.tracker.Delete(domain)
	return nil
}

func (lm *LifecycleManager) GetStatus(domain string) TLSStatus {
	return lm.tracker.Get(domain)
}
