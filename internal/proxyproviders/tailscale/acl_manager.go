// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package tailscale

import (
	"context"
	"fmt"
	"strings"

	"github.com/rs/zerolog"
	tailscale "tailscale.com/client/tailscale/v2"
)

const funnelAttr = "funnel"

type ACLManager struct {
	client *tailscale.Client
	log    zerolog.Logger
}

func NewACLManager(client *tailscale.Client, log zerolog.Logger) *ACLManager {
	if client == nil {
		return nil
	}
	return &ACLManager{
		client: client,
		log:    log.With().Str("component", "acl_manager").Logger(),
	}
}

func (m *ACLManager) EnsureTags(ctx context.Context, tags []string) error {
	if m == nil {
		return nil
	}
	acl, err := m.client.PolicyFile().Get(ctx)
	if err != nil {
		return fmt.Errorf("read ACL for auto-provision: %w — "+
			"the OAuth client needs the %q and %q scopes "+
			"(https://login.tailscale.com/admin/settings/oauth)", err, ScopePolicyRead, ScopePolicyWrite)
	}

	changed := false
	for _, tag := range tags {
		if _, ok := acl.TagOwners[tag]; !ok {
			if acl.TagOwners == nil {
				acl.TagOwners = make(map[string][]string)
			}
			acl.TagOwners[tag] = []string{"autogroup:admin"}
			changed = true
			m.log.Info().Str("tag", tag).Msg("auto-provisioning tag in ACL tagOwners")
		}
	}

	if !changed {
		return nil
	}

	return m.applyACL(ctx, acl)
}

func (m *ACLManager) EnsureFunnelAttribute(ctx context.Context, tag string) error {
	if m == nil {
		return nil
	}
	acl, err := m.client.PolicyFile().Get(ctx)
	if err != nil {
		return fmt.Errorf("read ACL for funnel check: %w — "+
			"the OAuth client needs the %q scope "+
			"(https://login.tailscale.com/admin/settings/oauth)", err, ScopePolicyRead)
	}

	for _, na := range acl.NodeAttrs {
		for _, a := range na.Attr {
			if a == funnelAttr {
				return nil
			}
		}
	}

	target := tag
	if target == "" {
		target = "autogroup:member"
	}
	acl.NodeAttrs = append(acl.NodeAttrs, tailscale.NodeAttrGrant{
		Target: []string{target},
		Attr:   []string{funnelAttr},
	})
	m.log.Info().Str("target", target).Msg("auto-provisioning Funnel attribute in ACL nodeAttrs")

	return m.applyACL(ctx, acl)
}

func (m *ACLManager) applyACL(ctx context.Context, acl *tailscale.ACL) error {
	etag := acl.ETag
	// The Tailscale SDK's Validate/Set accept an ACL value or HuJSON string via
	// a type switch; a *ACL pointer is rejected. Dereference before passing.
	if err := m.client.PolicyFile().Validate(ctx, *acl); err != nil {
		return fmt.Errorf("ACL validation failed (dry-run): %w — "+
			"the OAuth client needs the %q scope "+
			"(https://login.tailscale.com/admin/settings/oauth)", err, ScopePolicyWrite)
	}
	if err := m.client.PolicyFile().Set(ctx, *acl, etag); err != nil {
		return fmt.Errorf("write ACL: %w — "+
			"another process may have modified the policy file concurrently, "+
			"or the OAuth client needs the %q scope "+
			"(https://login.tailscale.com/admin/settings/oauth)", err, ScopePolicyWrite)
	}
	m.log.Info().Msg("ACL updated successfully")
	return nil
}

func parseTagsForACL(raw string) []string {
	var tags []string
	for _, t := range strings.Split(raw, ",") {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		if !strings.HasPrefix(t, "tag:") {
			t = "tag:" + t
		}
		tags = append(tags, t)
	}
	return tags
}
