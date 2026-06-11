// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package dashboard

import (
	"testing"
	"time"

	"github.com/almeidapaulopt/tsdproxy/internal/model"
	"github.com/almeidapaulopt/tsdproxy/internal/proxymanager"
	"github.com/almeidapaulopt/tsdproxy/internal/ui/pages"
)

// -- BuildDashboardView with empty/invisible proxies --------------------------

func TestBuildDashboardView_EmptyProxyList(t *testing.T) {
	t.Parallel()

	prefs := defaultPreferences()
	view := BuildDashboardView(proxymanager.ProxyList{}, prefs, "", false)

	if len(view.Items) != 0 {
		t.Fatalf("expected 0 items, got %d", len(view.Items))
	}
	if len(view.Groups) != 0 {
		t.Fatalf("expected 0 groups when not grouped, got %d", len(view.Groups))
	}
}

func TestBuildDashboardView_GroupedEmptyList(t *testing.T) {
	t.Parallel()

	prefs := defaultPreferences()
	prefs.Grouped = true

	view := BuildDashboardView(proxymanager.ProxyList{}, prefs, "", false)

	if len(view.Groups) != 0 {
		t.Fatalf("expected 0 groups for empty list, got %d", len(view.Groups))
	}
}

func TestBuildDashboardView_InvisibleProxyFiltered(t *testing.T) {
	t.Parallel()

	cfg := &model.Config{
		Hostname:  "hidden",
		Ports:     model.PortConfigList{},
		Dashboard: model.Dashboard{Visible: false},
		Tailscale: model.Tailscale{},
	}

	proxy := &proxymanager.Proxy{Config: cfg}
	proxies := proxymanager.ProxyList{"hidden": proxy}
	prefs := defaultPreferences()

	view := BuildDashboardView(proxies, prefs, "", false)

	if len(view.Items) != 0 {
		t.Fatalf("expected 0 items for invisible proxy, got %d", len(view.Items))
	}
}

func TestBuildDashboardView_MultipleInvisibleProxies(t *testing.T) {
	t.Parallel()

	proxies := make(proxymanager.ProxyList)
	for i := 0; i < 5; i++ { //nolint:mnd
		name := "hidden" + string(rune('a'+i))
		cfg := &model.Config{
			Hostname:  name,
			Ports:     model.PortConfigList{},
			Dashboard: model.Dashboard{Visible: false},
			Tailscale: model.Tailscale{},
		}
		proxies[name] = &proxymanager.Proxy{Config: cfg}
	}

	prefs := defaultPreferences()
	view := BuildDashboardView(proxies, prefs, "", false)

	if len(view.Items) != 0 {
		t.Fatalf("expected 0 items for invisible proxies, got %d", len(view.Items))
	}
}

// Note: Full BuildDashboardView tests with visible proxies require constructing
// proxymanager.Proxy instances with providerProxy set (an unexported interface field).
// GetURL() and GetAuthURL() delegate to providerProxy, which panics on nil.
// Those integration paths are covered by e2e tests in the e2e/ directory.

// -- matchesFilter additional edge cases --------------------------------------

func TestMatchesFilter_SearchCaseInsensitive(t *testing.T) {
	t.Parallel()

	data := pages.ProxyData{Label: "PostgreSQL Database"}
	prefs := defaultPreferences()

	if !matchesFilter(data, prefs, "postgresql") {
		t.Fatal("expected case-insensitive match on lowercase search")
	}
	if !matchesFilter(data, prefs, "DATABASE") {
		t.Fatal("expected case-insensitive match on uppercase search")
	}
}

func TestMatchesFilter_SearchEmptyString(t *testing.T) {
	t.Parallel()

	data := pages.ProxyData{Label: "My App"}
	prefs := defaultPreferences()

	if !matchesFilter(data, prefs, "") {
		t.Fatal("expected match with empty search string")
	}
}

func TestMatchesFilter_SearchPartialMatch(t *testing.T) {
	t.Parallel()

	data := pages.ProxyData{Label: "Redis Cache Server"}
	prefs := defaultPreferences()

	if !matchesFilter(data, prefs, "cache") {
		t.Fatal("expected partial substring match")
	}
}

func TestMatchesFilter_StatusAndHealthCombined(t *testing.T) {
	t.Parallel()

	data := pages.ProxyData{
		ProxyStatus:  model.ProxyStatusRunning,
		HealthStatus: "down",
		Label:        "My App",
	}
	prefs := model.Preferences{
		FilterStatus: "Running",
		FilterHealth: "down",
	}

	if !matchesFilter(data, prefs, "") {
		t.Fatal("expected match with both status and health filters")
	}
}

func TestMatchesFilter_StatusMatchHealthNoMatch(t *testing.T) {
	t.Parallel()

	data := pages.ProxyData{
		ProxyStatus:  model.ProxyStatusRunning,
		HealthStatus: "healthy",
	}
	prefs := model.Preferences{
		FilterStatus: "Running",
		FilterHealth: "down",
	}

	if matchesFilter(data, prefs, "") {
		t.Fatal("expected no match when health filter doesn't match")
	}
}

// -- sortItems additional coverage ---------------------------------------------

func TestSortItems_PinnedPriority(t *testing.T) {
	t.Parallel()

	items := []pages.ProxyViewItem{
		{Name: "alpha"},
		{Name: "bravo"},
		{Name: "charlie"},
		{Name: "delta"},
	}
	pinned := map[string]bool{"delta": true}
	sortItems(items, "name", pinned)

	if items[0].Name != "delta" {
		t.Fatalf("expected pinned 'delta' first, got %s", items[0].Name)
	}
	if items[1].Name != "alpha" || items[2].Name != "bravo" || items[3].Name != "charlie" {
		t.Fatalf("expected alpha,bravo,charlie after pinned, got %v", names(items[1:]))
	}
}

func TestSortItems_AllPinned(t *testing.T) {
	t.Parallel()

	items := []pages.ProxyViewItem{
		{Name: "c"},
		{Name: "a"},
		{Name: "b"},
	}
	pinned := map[string]bool{"a": true, "b": true, "c": true}
	sortItems(items, "name", pinned)

	if items[0].Name != "a" || items[1].Name != "b" || items[2].Name != "c" {
		t.Fatalf("expected a,b,c sorted within all-pinned, got %v", names(items))
	}
}

func TestSortItems_NilPinned(t *testing.T) {
	t.Parallel()

	items := []pages.ProxyViewItem{
		{Name: "b"},
		{Name: "a"},
	}
	sortItems(items, "name", nil)

	if items[0].Name != "a" || items[1].Name != "b" {
		t.Fatalf("expected a,b sorted, got %v", names(items))
	}
}

// -- groupItems additional coverage --------------------------------------------

func TestGroupItems_MultipleCategories(t *testing.T) {
	t.Parallel()

	items := []pages.ProxyViewItem{
		{Name: "app1", Category: "frontend"},
		{Name: "app2", Category: "backend"},
		{Name: "app3", Category: "frontend"},
		{Name: "app4", Category: "database"},
		{Name: "app5", Category: "backend"},
	}

	groups := groupItems(items)

	if len(groups) != 3 {
		t.Fatalf("expected 3 groups, got %d", len(groups))
	}
	if groups[0].Name != "frontend" {
		t.Fatalf("expected first group 'frontend', got %s", groups[0].Name)
	}
	if len(groups[0].Items) != 2 {
		t.Fatalf("expected 2 frontend items, got %d", len(groups[0].Items))
	}
}

func TestGroupItems_MixedUngroupedAndGrouped(t *testing.T) {
	t.Parallel()

	items := []pages.ProxyViewItem{
		{Name: "app1", Category: "web"},
		{Name: "app2", Category: ""},
		{Name: "app3", Category: "db"},
		{Name: "app4", Category: "  "},
	}

	groups := groupItems(items)

	if len(groups) != 3 {
		t.Fatalf("expected 3 groups (web, Ungrouped, db), got %d", len(groups))
	}

	var foundUngrouped bool
	for _, g := range groups {
		if g.Name == groupUngrouped {
			foundUngrouped = true
			if len(g.Items) != 2 {
				t.Fatalf("expected 2 ungrouped items, got %d", len(g.Items))
			}
		}
	}
	if !foundUngrouped {
		t.Fatal("expected 'Ungrouped' group for empty/whitespace categories")
	}
}

// -- pinnedSet additional coverage ---------------------------------------------

func TestPinnedSet_DuplicatesIgnored(t *testing.T) {
	t.Parallel()

	s := pinnedSet(model.Preferences{Pinned: []string{"a", "a", "a"}})
	if len(s) != 1 {
		t.Fatalf("expected 1 unique pinned item, got %d", len(s))
	}
	if !s["a"] {
		t.Fatal("expected 'a' to be pinned")
	}
}

// -- healthRank additional coverage --------------------------------------------

func TestHealthRank_Order(t *testing.T) {
	t.Parallel()

	if healthRank(healthHealthy) >= healthRank(healthUnknown) {
		t.Fatal("expected healthy < unknown")
	}
	if healthRank(healthUnknown) >= healthRank(filterDown) {
		t.Fatal("expected unknown < down")
	}
}

// -- healthValue additional coverage -------------------------------------------

func TestHealthValue_NonKnownNonEmpty(t *testing.T) {
	t.Parallel()

	if h := healthValue("degraded"); h != "degraded" {
		t.Fatalf("expected 'degraded' passed through, got %q", h)
	}
}

// -- formatAgo additional edge cases -------------------------------------------

func TestFormatAgo_AlmostOneMinute(t *testing.T) {
	t.Parallel()

	s := formatAgo(time.Now().Add(-30 * time.Second))
	if s != "just now" {
		t.Fatalf("expected 'just now' for <1m, got %q", s)
	}
}

// -- formatHealthStatus additional coverage ------------------------------------

func TestFormatHealthStatus_Down(t *testing.T) {
	t.Parallel()

	h, _ := formatHealthStatus(proxymanager.HealthResult{Status: proxymanager.HealthDown})
	if h != "down" {
		t.Fatalf("expected 'down', got %s", h)
	}
}

func TestFormatHealthStatus_Unknown(t *testing.T) {
	t.Parallel()

	// HealthUnknown is 0, which formatHealthStatus treats as "no status set"
	// and returns empty strings. This is correct: zero-value means "not checked".
	h, _ := formatHealthStatus(proxymanager.HealthResult{Status: proxymanager.HealthUnknown})
	if h != "" {
		t.Fatalf("expected empty string for zero-value HealthUnknown, got %q", h)
	}
}

func TestFormatHealthStatus_LatencyZero(t *testing.T) {
	t.Parallel()

	h, l := formatHealthStatus(proxymanager.HealthResult{
		Status:  proxymanager.HealthHealthy,
		Latency: 0,
	})
	if h != "healthy" {
		t.Fatalf("expected 'healthy', got %s", h)
	}
	if l != "" {
		t.Fatalf("expected empty latency for 0ms, got %q", l)
	}
}

// -- buildPortEntries additional coverage --------------------------------------

func TestBuildPortEntries_HTTP_DefaultPort(t *testing.T) {
	t.Parallel()

	entries := buildPortEntries(model.PortConfigList{
		"web": {ProxyProtocol: "http", ProxyPort: 80},
	}, "test.example.com")
	if entries[0].URL != "http://test.example.com" {
		t.Fatalf("expected http://test.example.com, got %s", entries[0].URL)
	}
}

func TestBuildPortEntries_MultiplePortTypes(t *testing.T) {
	t.Parallel()

	entries := buildPortEntries(model.PortConfigList{
		"https": {ProxyProtocol: "https", ProxyPort: 443},
		"http":  {ProxyProtocol: "http", ProxyPort: 80},
		"tcp":   {ProxyProtocol: "tcp", ProxyPort: 22},
	}, "host.ts.net")

	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(entries))
	}

	urls := make(map[string]bool)
	for _, e := range entries {
		urls[e.URL] = true
	}
	if !urls["https://host.ts.net"] {
		t.Fatal("expected https://host.ts.net in entries")
	}
	if !urls["http://host.ts.net"] {
		t.Fatal("expected http://host.ts.net in entries")
	}
	if !urls["tcp://host.ts.net:22"] {
		t.Fatal("expected tcp://host.ts.net:22 in entries")
	}
}
