// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package dnsproviders

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateCNAME_EmptyDomain(t *testing.T) {
	_, err := ValidateCNAME(context.Background(), "", "target.example.com")
	require.NoError(t, err)
}

func TestPollCNAME_Timeout(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	err := PollCNAME(ctx, "nonexistent.invalid", "target.example.com.", 50*time.Millisecond)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "deadline exceeded")
}

func TestEnsureTrailingDot(t *testing.T) {
	assert.Equal(t, "example.com.", ensureTrailingDot("example.com"))
	assert.Equal(t, "example.com.", ensureTrailingDot("example.com."))
	assert.Equal(t, "", ensureTrailingDot(""))
}
