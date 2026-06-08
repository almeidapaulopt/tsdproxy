// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package dnsproviders

import (
	"context"
	"fmt"
	"time"
)

const maxBackoff = 30 * time.Second

// Retry executes op with exponential backoff until it succeeds or maxRetries is exceeded.
func Retry(ctx context.Context, op func() error, maxRetries int, initialBackoff time.Duration) error {
	var lastErr error

	for attempt := 0; attempt <= maxRetries; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}

		lastErr = op()
		if lastErr == nil {
			return nil
		}

		if attempt < maxRetries {
			backoff := initialBackoff
			for i := 0; i < attempt; i++ {
				next := backoff * 2 //nolint:mnd
				if next <= backoff || next > maxBackoff {
					backoff = maxBackoff
					break
				}
				backoff = next
			}

			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(backoff):
			}
		}
	}

	return fmt.Errorf("retry: max retries (%d) exceeded: %w", maxRetries, lastErr)
}
