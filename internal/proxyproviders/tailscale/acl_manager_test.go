// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package tailscale

import (
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
)

func TestParseTagsForACL_SingleTag(t *testing.T) {
	t.Parallel()
	tags := parseTagsForACL("tag:myapp")
	assert.Equal(t, []string{"tag:myapp"}, tags)
}

func TestParseTagsForACL_MultipleTags(t *testing.T) {
	t.Parallel()
	tags := parseTagsForACL("tag:web, tag:prod")
	assert.Equal(t, []string{"tag:web", "tag:prod"}, tags)
}

func TestParseTagsForACL_AddsPrefix(t *testing.T) {
	t.Parallel()
	tags := parseTagsForACL("myapp, prod")
	assert.Equal(t, []string{"tag:myapp", "tag:prod"}, tags)
}

func TestParseTagsForACL_Empty(t *testing.T) {
	t.Parallel()
	tags := parseTagsForACL("")
	assert.Nil(t, tags)
}

func TestParseTagsForACL_WhitespaceOnly(t *testing.T) {
	t.Parallel()
	tags := parseTagsForACL("  , ,  ")
	assert.Nil(t, tags)
}

func TestNewACLManager_NilClientReturnsNil(t *testing.T) {
	t.Parallel()
	mgr := NewACLManager(nil, zerolog.Nop())
	assert.Nil(t, mgr)
}
