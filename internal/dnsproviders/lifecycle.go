// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package dnsproviders

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/almeidapaulopt/tsdproxy/internal/lifecycle"
)

const dnsValidationRetries = 10

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

func (lm *LifecycleManager) SetupDNS(ctx context.Context, provider Provider, domain, targetHostname string) error {
	lm.tracker.Set(domain, DNSStatusPending)

	if err := provider.CreateRecord(ctx, domain, "CNAME", targetHostname); err != nil {
		lm.tracker.Set(domain, DNSStatusError)
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
	}, dnsValidationRetries, 2*time.Second); err != nil {
		lm.tracker.Set(domain, DNSStatusError)
		return fmt.Errorf("cname propagation for %s: %w", domain, err)
	}

	lm.tracker.Set(domain, DNSStatusActive)
	return nil
}

func (lm *LifecycleManager) CleanupDNS(ctx context.Context, provider Provider, domain string) error {
	if !lm.cleanup {
		return nil
	}

	if err := provider.DeleteRecord(ctx, domain, "CNAME"); err != nil {
		return fmt.Errorf("delete cname for %s: %w", domain, err)
	}

	lm.tracker.Delete(domain)
	return nil
}

func (lm *LifecycleManager) GetStatus(domain string) DNSStatus {
	return lm.tracker.Get(domain)
}
