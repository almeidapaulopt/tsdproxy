// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package dashboard

import (
	"os"
	"testing"
	"time"

	"github.com/almeidapaulopt/tsdproxy/internal/model"
	"github.com/almeidapaulopt/tsdproxy/internal/proxymanager"
	"github.com/almeidapaulopt/tsdproxy/internal/ui/pages"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
)

func testingLogger(t *testing.T) zerolog.Logger {
	t.Helper()
	return zerolog.New(zerolog.NewTestWriter(t))
}

// -- defaultPreferences --------------------------------------------------------

func TestDefaultPreferences(t *testing.T) {
	p := defaultPreferences()
	if !p.Dark {
		t.Fatal("expected dark mode default true")
	}
	if p.View != "card" {
		t.Fatalf("expected card view, got %s", p.View)
	}
	if p.Sort != "name" {
		t.Fatalf("expected sort by name, got %s", p.Sort)
	}
	if p.FilterStatus != filterAll {
		t.Fatalf("expected filter all status, got %s", p.FilterStatus)
	}
	if p.FilterHealth != filterAll {
		t.Fatalf("expected filter all health, got %s", p.FilterHealth)
	}
}

// -- validatePrefs -------------------------------------------------------------

func TestValidatePrefs_Valid(t *testing.T) {
	p := model.Preferences{
		Dark:         false,
		View:         "list",
		Sort:         "status",
		FilterStatus: "Running",
		FilterHealth: "healthy",
		Pinned:       []string{"a", "b"},
	}
	validatePrefs(&p)

	if p.View != "list" {
		t.Fatalf("expected list, got %s", p.View)
	}
	if p.Sort != "status" {
		t.Fatalf("expected status, got %s", p.Sort)
	}
}

func TestValidatePrefs_MigrationCompactToList(t *testing.T) {
	p := model.Preferences{View: "compact"}
	validatePrefs(&p)
	if p.View != "list" {
		t.Fatalf("expected compact migrated to list, got %s", p.View)
	}
}

func TestValidatePrefs_InvalidView(t *testing.T) {
	p := model.Preferences{View: "garbage"}
	validatePrefs(&p)
	if p.View != "card" {
		t.Fatalf("expected fallback to card, got %s", p.View)
	}
}

func TestValidatePrefs_InvalidSort(t *testing.T) {
	p := model.Preferences{Sort: "garbage"}
	validatePrefs(&p)
	if p.Sort != "name" {
		t.Fatalf("expected fallback to name, got %s", p.Sort)
	}
}

func TestValidatePrefs_InvalidFilterStatus(t *testing.T) {
	p := model.Preferences{FilterStatus: "garbage"}
	validatePrefs(&p)
	if p.FilterStatus != filterAll {
		t.Fatalf("expected fallback to all, got %s", p.FilterStatus)
	}
}

func TestValidatePrefs_InvalidFilterHealth(t *testing.T) {
	p := model.Preferences{FilterHealth: "garbage"}
	validatePrefs(&p)
	if p.FilterHealth != filterAll {
		t.Fatalf("expected fallback to all, got %s", p.FilterHealth)
	}
}

func TestValidatePrefs_DedupPinned(t *testing.T) {
	p := model.Preferences{Pinned: []string{"a", "b", "a", "", "c", "b"}}
	validatePrefs(&p)
	expected := []string{"a", "b", "c"}
	if len(p.Pinned) != len(expected) {
		t.Fatalf("expected %d pinned, got %d: %v", len(expected), len(p.Pinned), p.Pinned)
	}
	for i, v := range expected {
		if p.Pinned[i] != v {
			t.Fatalf("expected pinned[%d]=%s, got %s", i, v, p.Pinned[i])
		}
	}
}

// -- normalizeUserID -----------------------------------------------------------

func TestNormalizeUserID_Valid(t *testing.T) {
	got := normalizeUserID("user123_ABC-def")
	if got != "user123_ABC-def" {
		t.Fatalf("expected user123_ABC-def, got %s", got)
	}
}

func TestNormalizeUserID_Invalid(t *testing.T) {
	got := normalizeUserID("../../etc/passwd")
	if got != "_invalid" {
		t.Fatalf("expected _invalid, got %s", got)
	}
}

func TestNormalizeUserID_Empty(t *testing.T) {
	got := normalizeUserID("")
	if got != "_invalid" {
		t.Fatalf("expected _invalid, got %s", got)
	}
}

// -- healthRank / healthValue --------------------------------------------------

func TestHealthRank_Known(t *testing.T) {
	if r := healthRank("healthy"); r != 0 {
		t.Fatalf("expected 0, got %d", r)
	}
	if r := healthRank("unknown"); r != 1 {
		t.Fatalf("expected 1, got %d", r)
	}
	if r := healthRank("down"); r != 2 {
		t.Fatalf("expected 2, got %d", r)
	}
}

func TestHealthRank_Unknown(t *testing.T) {
	if r := healthRank("nonexistent"); r != len(healthOrder) {
		t.Fatalf("expected %d, got %d", len(healthOrder), r)
	}
}

func TestHealthValue_Empty(t *testing.T) {
	if h := healthValue(""); h != healthUnknown {
		t.Fatalf("expected %s, got %s", healthUnknown, h)
	}
}

func TestHealthValue_NonEmpty(t *testing.T) {
	if h := healthValue("healthy"); h != "healthy" {
		t.Fatalf("expected healthy, got %s", h)
	}
}

// -- pinnedSet -----------------------------------------------------------------

func TestPinnedSet_Empty(t *testing.T) {
	s := pinnedSet(model.Preferences{})
	if len(s) != 0 {
		t.Fatalf("expected empty set, got %d", len(s))
	}
}

func TestPinnedSet_WithValues(t *testing.T) {
	s := pinnedSet(model.Preferences{Pinned: []string{"a", "b", "c"}})
	if !s["a"] || !s["b"] || !s["c"] {
		t.Fatal("expected a, b, c to be pinned")
	}
	if s["d"] {
		t.Fatal("expected d not to be pinned")
	}
}

// -- matchesFilter -------------------------------------------------------------

func TestMatchesFilter_NoFilter(t *testing.T) {
	data := pages.ProxyData{Name: "test", ProxyStatus: model.ProxyStatusRunning}
	prefs := defaultPreferences()
	if !matchesFilter(data, prefs, "") {
		t.Fatal("expected match with no filters")
	}
}

func TestMatchesFilter_FilterStatusMatch(t *testing.T) {
	data := pages.ProxyData{Name: "test", ProxyStatus: model.ProxyStatusRunning}
	prefs := defaultPreferences()
	prefs.FilterStatus = "Running"
	if !matchesFilter(data, prefs, "") {
		t.Fatal("expected match when status filter matches")
	}
}

func TestMatchesFilter_FilterStatusNoMatch(t *testing.T) {
	data := pages.ProxyData{Name: "test", ProxyStatus: model.ProxyStatusStopped}
	prefs := defaultPreferences()
	prefs.FilterStatus = "Running"
	if matchesFilter(data, prefs, "") {
		t.Fatal("expected no match when status filter differs")
	}
}

func TestMatchesFilter_FilterHealthMatch(t *testing.T) {
	data := pages.ProxyData{Name: "test", HealthStatus: "healthy"}
	prefs := defaultPreferences()
	prefs.FilterHealth = "healthy"
	if !matchesFilter(data, prefs, "") {
		t.Fatal("expected match when health filter matches")
	}
}

func TestMatchesFilter_FilterHealthNoMatch(t *testing.T) {
	data := pages.ProxyData{Name: "test", HealthStatus: "down"}
	prefs := defaultPreferences()
	prefs.FilterHealth = "healthy"
	if matchesFilter(data, prefs, "") {
		t.Fatal("expected no match when health filter differs")
	}
}

func TestMatchesFilter_FilterHealthBlankIsUnknown(t *testing.T) {
	data := pages.ProxyData{Name: "test", HealthStatus: ""}
	prefs := defaultPreferences()
	prefs.FilterHealth = healthUnknown
	if !matchesFilter(data, prefs, "") {
		t.Fatal("expected blank health to match 'unknown' filter")
	}
}

func TestMatchesFilter_SearchMatch(t *testing.T) {
	data := pages.ProxyData{Label: "My Test Proxy"}
	prefs := defaultPreferences()
	if !matchesFilter(data, prefs, "test") {
		t.Fatal("expected case-insensitive search match")
	}
}

func TestMatchesFilter_SearchNoMatch(t *testing.T) {
	data := pages.ProxyData{Label: "Something Else"}
	prefs := defaultPreferences()
	if matchesFilter(data, prefs, "test") {
		t.Fatal("expected no match when search differs")
	}
}

func TestMatchesFilter_AllFilters(t *testing.T) {
	data := pages.ProxyData{Name: "test", ProxyStatus: model.ProxyStatusRunning, HealthStatus: "healthy", Label: "My App"}
	prefs := model.Preferences{
		FilterStatus: "Running",
		FilterHealth: "healthy",
		Sort:         "name",
		View:         "card",
		Dark:         true,
	}
	if !matchesFilter(data, prefs, "app") {
		t.Fatal("expected match with all filters")
	}
}

// -- sortItems -----------------------------------------------------------------

func TestSortItems_ByName(t *testing.T) {
	items := []pages.ProxyViewItem{
		{Name: "z", Data: pages.ProxyData{Name: "z"}},
		{Name: "a", Data: pages.ProxyData{Name: "a"}},
		{Name: "m", Data: pages.ProxyData{Name: "m"}},
	}
	sortItems(items, "name", nil)
	if items[0].Name != "a" || items[1].Name != "m" || items[2].Name != "z" {
		t.Fatalf("expected a,m,z sorted, got %v", names(items))
	}
}

func TestSortItems_PinnedFirst(t *testing.T) {
	items := []pages.ProxyViewItem{
		{Name: "b"},
		{Name: "a"},
		{Name: "c"},
	}
	pinned := map[string]bool{"c": true, "a": true}
	sortItems(items, "name", pinned)
	if items[0].Name != "a" || items[1].Name != "c" || items[2].Name != "b" {
		t.Fatalf("expected pinned a,c first then b, got %v", names(items))
	}
}

func TestSortItems_ByStatus(t *testing.T) {
	items := []pages.ProxyViewItem{
		{Name: "b", Data: pages.ProxyData{Name: "b", ProxyStatus: model.ProxyStatusRunning}},
		{Name: "a", Data: pages.ProxyData{Name: "a", ProxyStatus: model.ProxyStatusError}},
	}
	sortItems(items, sortStatus, nil)
	if items[0].Name != "a" { // Error < Running alphabetically
		t.Fatalf("expected Error first, got %s", items[0].Name)
	}
}

func TestSortItems_ByHealth(t *testing.T) {
	items := []pages.ProxyViewItem{
		{Name: "a", Data: pages.ProxyData{Name: "a", HealthStatus: "down"}},
		{Name: "b", Data: pages.ProxyData{Name: "b", HealthStatus: "healthy"}},
	}
	sortItems(items, sortHealth, nil)
	if items[0].Data.HealthStatus != "healthy" {
		t.Fatalf("expected healthy first, got %s", items[0].Data.HealthStatus)
	}
}

func TestSortItems_ByHealthTiebreakByName(t *testing.T) {
	items := []pages.ProxyViewItem{
		{Name: "z", Data: pages.ProxyData{Name: "z", HealthStatus: "healthy"}},
		{Name: "a", Data: pages.ProxyData{Name: "a", HealthStatus: "healthy"}},
	}
	sortItems(items, sortHealth, nil)
	if items[0].Name != "a" || items[1].Name != "z" {
		t.Fatalf("expected a,z in order, got %v", names(items))
	}
}

func TestSortItems_ByProvider(t *testing.T) {
	items := []pages.ProxyViewItem{
		{Name: "b", Data: pages.ProxyData{Name: "b", TargetProvider: "list"}},
		{Name: "a", Data: pages.ProxyData{Name: "a", TargetProvider: "docker"}},
	}
	sortItems(items, "provider", nil)
	if items[0].Data.TargetProvider != "docker" {
		t.Fatalf("expected docker first (alphabetically), got %s", items[0].Data.TargetProvider)
	}
}

func names(items []pages.ProxyViewItem) []string {
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = it.Name
	}
	return out
}

// -- groupItems ----------------------------------------------------------------

func TestGroupItems_Empty(t *testing.T) {
	groups := groupItems(nil)
	if len(groups) != 0 {
		t.Fatalf("expected 0 groups, got %d", len(groups))
	}
}

func TestGroupItems_SingleGroup(t *testing.T) {
	items := []pages.ProxyViewItem{
		{Name: "a", Category: "web"},
		{Name: "b", Category: "web"},
	}
	groups := groupItems(items)
	if len(groups) != 1 {
		t.Fatalf("expected 1 group, got %d", len(groups))
	}
	if groups[0].Name != "web" {
		t.Fatalf("expected web group, got %s", groups[0].Name)
	}
	if len(groups[0].Items) != 2 {
		t.Fatalf("expected 2 items in group, got %d", len(groups[0].Items))
	}
}

func TestGroupItems_MultipleGroups(t *testing.T) {
	items := []pages.ProxyViewItem{
		{Name: "a", Category: "web"},
		{Name: "b", Category: "db"},
		{Name: "c", Category: "web"},
	}
	groups := groupItems(items)
	if len(groups) != 2 {
		t.Fatalf("expected 2 groups, got %d", len(groups))
	}
}

func TestGroupItems_UngroupedCategory(t *testing.T) {
	items := []pages.ProxyViewItem{
		{Name: "a", Category: ""},
	}
	groups := groupItems(items)
	if len(groups) != 1 {
		t.Fatalf("expected 1 group, got %d", len(groups))
	}
	if groups[0].Name != "Ungrouped" {
		t.Fatalf("expected Ungrouped, got %s", groups[0].Name)
	}
}

func TestGroupItems_WhitespaceCategory(t *testing.T) {
	items := []pages.ProxyViewItem{
		{Name: "a", Category: "  "},
	}
	groups := groupItems(items)
	if len(groups) != 1 {
		t.Fatalf("expected 1 group, got %d", len(groups))
	}
	if groups[0].Name != "Ungrouped" {
		t.Fatalf("expected Ungrouped, got %s", groups[0].Name)
	}
}

func TestGroupItems_PreservesOrder(t *testing.T) {
	items := []pages.ProxyViewItem{
		{Name: "a", Category: "db"},
		{Name: "b", Category: "web"},
		{Name: "c", Category: "cache"},
	}
	groups := groupItems(items)
	if groups[0].Name != "db" || groups[1].Name != "web" || groups[2].Name != "cache" {
		t.Fatalf("expected original order db,web,cache, got %v", groupNames(groups))
	}
}

func groupNames(groups []pages.DashboardGroup) []string {
	out := make([]string, len(groups))
	for i, g := range groups {
		out[i] = g.Name
	}
	return out
}

// -- formatAgo -----------------------------------------------------------------

func TestFormatAgo_Zero(t *testing.T) {
	if s := formatAgo(time.Time{}); s != "" {
		t.Fatalf("expected empty string, got %s", s)
	}
}

func TestFormatAgo_JustNow(t *testing.T) {
	if s := formatAgo(time.Now()); s != "just now" {
		t.Fatalf("expected 'just now', got %s", s)
	}
}

func TestFormatAgo_Minutes(t *testing.T) {
	s := formatAgo(time.Now().Add(-5 * time.Minute))
	if s != "5m ago" {
		t.Fatalf("expected '5m ago', got %s", s)
	}
}

func TestFormatAgo_1Minute(t *testing.T) {
	s := formatAgo(time.Now().Add(-1 * time.Minute))
	if s != "1m ago" {
		t.Fatalf("expected '1m ago', got %s", s)
	}
}

func TestFormatAgo_Hours(t *testing.T) {
	s := formatAgo(time.Now().Add(-3 * time.Hour))
	if s != "3h ago" {
		t.Fatalf("expected '3h ago', got %s", s)
	}
}

func TestFormatAgo_1Hour(t *testing.T) {
	s := formatAgo(time.Now().Add(-1 * time.Hour))
	if s != "1h ago" {
		t.Fatalf("expected '1h ago', got %s", s)
	}
}

func TestFormatAgo_Days(t *testing.T) {
	s := formatAgo(time.Now().Add(-48 * time.Hour))
	if s != "2d ago" {
		t.Fatalf("expected '2d ago', got %s", s)
	}
}

func TestFormatAgo_1Day(t *testing.T) {
	s := formatAgo(time.Now().Add(-24 * time.Hour))
	// May be 23h or 24h depending on timing — check it ends with " ago"
	if s != "1d ago" {
		t.Fatalf("expected '1d ago', got %s", s)
	}
}

// -- formatHealthStatus --------------------------------------------------------

func TestFormatHealthStatus_Zero(t *testing.T) {
	h, l, _ := formatHealthStatus(proxymanager.HealthResult{})
	if h != "" || l != "" {
		t.Fatalf("expected empty strings, got %q %q", h, l)
	}
}

func TestFormatHealthStatus_Healthy(t *testing.T) {
	h, l, _ := formatHealthStatus(proxymanager.HealthResult{Status: proxymanager.HealthHealthy})
	if h != "healthy" {
		t.Fatalf("expected healthy, got %s", h)
	}
	if l != "" {
		t.Fatalf("expected no latency, got %s", l)
	}
}

func TestFormatHealthStatus_WithLatency(t *testing.T) {
	h, l, _ := formatHealthStatus(proxymanager.HealthResult{Status: proxymanager.HealthHealthy, Latency: 50 * time.Millisecond})
	if h != "healthy" {
		t.Fatalf("expected healthy, got %s", h)
	}
	if l != "(50ms)" {
		t.Fatalf("expected (50ms), got %s", l)
	}
}

// -- buildPortEntries ----------------------------------------------------------

func TestBuildPortEntries_Empty(t *testing.T) {
	entries := buildPortEntries(model.PortConfigList{}, "test.example.com")
	if len(entries) != 0 {
		t.Fatalf("expected 0 entries, got %d", len(entries))
	}
}

func TestBuildPortEntries_HTTPS(t *testing.T) {
	entries := buildPortEntries(model.PortConfigList{
		"web": {ProxyProtocol: "https", ProxyPort: 443},
	}, "test.example.com")
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].URL != "https://test.example.com" {
		t.Fatalf("expected https://test.example.com, got %s", entries[0].URL)
	}
}

func TestBuildPortEntries_HTTPS_NonDefaultPort(t *testing.T) {
	entries := buildPortEntries(model.PortConfigList{
		"web": {ProxyProtocol: "https", ProxyPort: 8443},
	}, "test.example.com")
	if entries[0].URL != "https://test.example.com:8443" {
		t.Fatalf("expected https://test.example.com:8443, got %s", entries[0].URL)
	}
}

func TestBuildPortEntries_HTTP_NonDefaultPort(t *testing.T) {
	entries := buildPortEntries(model.PortConfigList{
		"web": {ProxyProtocol: "http", ProxyPort: 8080},
	}, "test.example.com")
	if entries[0].URL != "http://test.example.com:8080" {
		t.Fatalf("expected http://test.example.com:8080, got %s", entries[0].URL)
	}
}

func TestBuildPortEntries_TCP(t *testing.T) {
	entries := buildPortEntries(model.PortConfigList{
		"ssh": {ProxyProtocol: "tcp", ProxyPort: 22},
	}, "test.example.com")
	if entries[0].URL != "tcp://test.example.com:22" {
		t.Fatalf("expected tcp://test.example.com:22, got %s", entries[0].URL)
	}
}

func TestBuildPortEntries_UDP(t *testing.T) {
	entries := buildPortEntries(model.PortConfigList{
		"voip": {ProxyProtocol: "udp", ProxyPort: 5060},
	}, "test.example.com")
	if entries[0].URL != "udp://test.example.com:5060" {
		t.Fatalf("expected udp://test.example.com:5060, got %s", entries[0].URL)
	}
}

func TestBuildPortEntries_Mixed(t *testing.T) {
	entries := buildPortEntries(model.PortConfigList{
		"https": {ProxyProtocol: "https", ProxyPort: 443},
		"http":  {ProxyProtocol: "http", ProxyPort: 80, IsRedirect: true},
	}, "test.example.com")
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
}

// -- EventType.String ----------------------------------------------------------

func TestEventType_String(t *testing.T) {
	tests := []struct {
		want string
		et   EventType
	}{
		{et: EventNotify, want: "notify"},
		{et: EventScrollLogs, want: "scroll-logs"},
		{et: EventTrimLogs, want: "trim-logs"},
		{et: EventHTMXListRefresh, want: "list-refresh"},
		{et: EventHTMXCardUpdate, want: "card-update"},
		{et: EventConnID, want: "conn-id"},
		{et: EventType(99), want: "unknown"},
	}
	for _, tt := range tests {
		if got := tt.et.String(); got != tt.want {
			t.Fatalf("EventType(%d).String() = %s, want %s", tt.et, got, tt.want)
		}
	}
}

// -- clientNeedsFullRender -----------------------------------------------------

func TestClientNeedsFullRender_SortByName(t *testing.T) {
	cp := clientPrefs{prefs: defaultPreferences()}
	if clientNeedsFullRender(cp, model.ProxyEvent{}) {
		t.Fatal("expected false for sort by name with default filter")
	}
}

func TestClientNeedsFullRender_SortByStatus(t *testing.T) {
	p := defaultPreferences()
	p.Sort = sortStatus
	cp := clientPrefs{prefs: p}
	if !clientNeedsFullRender(cp, model.ProxyEvent{}) {
		t.Fatal("expected true for sort by status")
	}
}

func TestClientNeedsFullRender_SortByHealth(t *testing.T) {
	p := defaultPreferences()
	p.Sort = sortHealth
	cp := clientPrefs{prefs: p}
	if !clientNeedsFullRender(cp, model.ProxyEvent{}) {
		t.Fatal("expected true for sort by health")
	}
}

func TestClientNeedsFullRender_FilterStatusChanged(t *testing.T) {
	p := defaultPreferences()
	p.FilterStatus = "Running"
	cp := clientPrefs{prefs: p}
	event := model.ProxyEvent{Status: model.ProxyStatusRunning, OldStatus: model.ProxyStatusStopped}
	if !clientNeedsFullRender(cp, event) {
		t.Fatal("expected true when filter status changes")
	}
}

func TestClientNeedsFullRender_FilterStatusUnchanged(t *testing.T) {
	p := defaultPreferences()
	p.FilterStatus = "Running"
	cp := clientPrefs{prefs: p}
	event := model.ProxyEvent{Status: model.ProxyStatusRunning, OldStatus: model.ProxyStatusRunning}
	if clientNeedsFullRender(cp, event) {
		t.Fatal("expected false when filter status unchanged")
	}
}

func TestClientNeedsFullRender_Grouped(t *testing.T) {
	p := defaultPreferences()
	p.Grouped = true
	cp := clientPrefs{prefs: p}
	if !clientNeedsFullRender(cp, model.ProxyEvent{}) {
		t.Fatal("expected true when grouped")
	}
}

func TestClientNeedsFullRender_AllDefaults(t *testing.T) {
	cp := clientPrefs{prefs: defaultPreferences()}
	event := model.ProxyEvent{Status: model.ProxyStatusRunning, OldStatus: model.ProxyStatusStopped}
	if clientNeedsFullRender(cp, event) {
		t.Fatal("expected false with defaults and status not filtered")
	}
}

// -- PreferencesStore ----------------------------------------------------------

func TestNewPreferencesStore(t *testing.T) {
	dir := t.TempDir()
	s, err := NewPreferencesStore(dir, testingLogger(t))
	if err != nil {
		t.Fatalf("NewPreferencesStore failed: %v", err)
	}
	if s == nil {
		t.Fatal("expected non-nil store")
	}
}

func TestPreferencesStore_Load_NewUser(t *testing.T) {
	dir := t.TempDir()
	s, err := NewPreferencesStore(dir, testingLogger(t))
	if err != nil {
		t.Fatalf("NewPreferencesStore failed: %v", err)
	}
	p, err := s.Load("newuser")
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if !p.Dark {
		t.Fatal("expected dark mode default true")
	}
}

func TestPreferencesStore_SaveAndLoad(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewPreferencesStore(dir, testingLogger(t))

	prefs := model.Preferences{
		Dark:         false,
		View:         "list",
		Sort:         "status",
		FilterStatus: "Running",
		FilterHealth: "healthy",
		Pinned:       []string{"a", "b"},
	}
	if err := s.Save("user1", prefs); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	loaded, err := s.Load("user1")
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if loaded.Dark != false {
		t.Fatalf("expected dark=false, got %v", loaded.Dark)
	}
	if loaded.View != "list" {
		t.Fatalf("expected view=list, got %s", loaded.View)
	}
	if loaded.Sort != "status" {
		t.Fatalf("expected sort=status, got %s", loaded.Sort)
	}
}

func TestPreferencesStore_Load_CachesAfterFirstLoad(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewPreferencesStore(dir, testingLogger(t))

	p, _ := s.Load("cacheduser")
	p.View = "list"
	require.NoError(t, s.Save("cacheduser", p))

	p2, _ := s.Load("cacheduser")
	if p2.View != "list" {
		t.Fatalf("expected cached list, got %s", p2.View)
	}
}

func TestPreferencesStore_LoadFromDiskAfterCacheMiss(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewPreferencesStore(dir, testingLogger(t))

	prefs := model.Preferences{View: "list", Sort: "status"}
	require.NoError(t, s.Save("diskuser", prefs))

	s2, _ := NewPreferencesStore(dir, testingLogger(t))
	loaded, err := s2.Load("diskuser")
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if loaded.View != "list" {
		t.Fatalf("expected list, got %s", loaded.View)
	}
}

func TestPreferencesStore_Save_InvalidUserID(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewPreferencesStore(dir, testingLogger(t))

	err := s.Save("../invalid", model.Preferences{View: "compact"})
	if err != nil {
		t.Fatalf("Save with invalid userID should not error: %v", err)
	}

	s2, _ := NewPreferencesStore(dir, testingLogger(t))
	p, err := s2.Load("../invalid")
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if p.View != "list" {
		t.Fatalf("expected list (migrated from compact), got %s", p.View)
	}
}

func TestPreferencesStore_Update(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewPreferencesStore(dir, testingLogger(t))

	err := s.Update("updateuser", func(p *model.Preferences) {
		p.View = "list"
		p.Sort = "status"
	})
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	p, _ := s.Load("updateuser")
	if p.View != "list" || p.Sort != "status" {
		t.Fatalf("expected list/status, got %s/%s", p.View, p.Sort)
	}
}

func TestPreferencesStore_TogglePin_Add(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewPreferencesStore(dir, testingLogger(t))

	p, err := s.TogglePin("pinuser", "proxy1")
	if err != nil {
		t.Fatalf("TogglePin failed: %v", err)
	}
	if len(p.Pinned) != 1 || p.Pinned[0] != "proxy1" {
		t.Fatalf("expected [proxy1], got %v", p.Pinned)
	}
}

func TestPreferencesStore_TogglePin_Remove(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewPreferencesStore(dir, testingLogger(t))

	_, err := s.TogglePin("pinuser2", "proxy1")
	require.NoError(t, err)
	p, err := s.TogglePin("pinuser2", "proxy1")
	if err != nil {
		t.Fatalf("TogglePin failed: %v", err)
	}
	if len(p.Pinned) != 0 {
		t.Fatalf("expected empty, got %v", p.Pinned)
	}
}

func TestPreferencesStore_Load_CorruptFile(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewPreferencesStore(dir, testingLogger(t))

	require.NoError(t, s.Save("corrupt", model.Preferences{View: "list"}))
	storeDir := dir + "/dashboard/preferences"
	if err := os.WriteFile(storeDir+"/corrupt.json", []byte("not json"), 0o600); err != nil { //nolint:mnd
		t.Fatalf("WriteFile failed: %v", err)
	}

	cacheDir := t.TempDir()
	s2, _ := NewPreferencesStore(cacheDir, testingLogger(t))
	require.NoError(t, os.MkdirAll(cacheDir+"/dashboard/preferences", 0o700))
	require.NoError(t, os.WriteFile(cacheDir+"/dashboard/preferences/corrupt.json", []byte("not json"), 0o600))

	p, err := s2.Load("corrupt")
	if err != nil {
		t.Fatalf("Load of corrupt file should not error: %v", err)
	}
	if p.View != "card" {
		t.Fatalf("expected defaults after corrupt file, got %s", p.View)
	}
}
