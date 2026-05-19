// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package dashboard

import (
	"sort"
	"strings"

	"github.com/almeidapaulopt/tsdproxy/internal/model"
	"github.com/almeidapaulopt/tsdproxy/internal/proxymanager"
	"github.com/almeidapaulopt/tsdproxy/internal/ui/pages"
)

const (
	filterAll     = "all"
	sortStatus    = "status"
	sortHealth    = "health"
	healthUnknown = "unknown"
	swapOuterHTML = "outerHTML"
)

var healthOrder = map[string]int{"healthy": 0, healthUnknown: 1, "down": 2} //nolint:mnd

func healthRank(h string) int {
	if r, ok := healthOrder[h]; ok {
		return r
	}
	return len(healthOrder)
}

func pinnedSet(prefs model.Preferences) map[string]bool {
	m := make(map[string]bool, len(prefs.Pinned))
	for _, p := range prefs.Pinned {
		m[p] = true
	}
	return m
}

// BuildDashboardView builds a fully-rendered dashboard view from proxy state.
// It produces pages.DashboardView directly, eliminating the need for a separate
// conversion layer — filtering, sorting, grouping, and ProxyData rendering
// all happen in a single pass.
func BuildDashboardView(
	proxies proxymanager.ProxyList,
	prefs model.Preferences,
	search string,
) pages.DashboardView {
	//
	pinned := pinnedSet(prefs)

	var items []pages.ProxyViewItem
	for name, p := range proxies {
		if !p.Config.Dashboard.Visible {
			continue
		}
		data := buildProxyDataFromProxy(name, p, pinned)
		if !matchesFilter(data, prefs, search) {
			continue
		}
		items = append(items, pages.ProxyViewItem{
			Name:     name,
			Category: p.Config.Dashboard.Category,
			Data:     data,
		})
	}

	sortItems(items, prefs.Sort, pinned)

	view := pages.DashboardView{Items: items}

	if prefs.Grouped {
		view.Groups = groupItems(items)
	}

	return view
}

func matchesFilter(data pages.ProxyData, prefs model.Preferences, search string) bool {
	if prefs.FilterStatus != filterAll {
		if data.ProxyStatus.String() != prefs.FilterStatus {
			return false
		}
	}

	if prefs.FilterHealth != filterAll {
		h := healthValue(data.HealthStatus)
		if h != prefs.FilterHealth {
			return false
		}
	}

	if search != "" {
		if !strings.Contains(strings.ToLower(data.Label), strings.ToLower(search)) {
			return false
		}
	}

	return true
}

func healthValue(h string) string {
	if h == "" {
		return healthUnknown
	}
	return h
}

func sortItems(items []pages.ProxyViewItem, sortKey string, pinned map[string]bool) {
	sort.SliceStable(items, func(i, j int) bool {
		iPinned := pinned[items[i].Name]
		jPinned := pinned[items[j].Name]
		if iPinned != jPinned {
			return iPinned
		}

		switch sortKey {
		case sortStatus:
			return items[i].Data.ProxyStatus.String() < items[j].Data.ProxyStatus.String()
		case "provider":
			return items[i].Data.TargetProvider < items[j].Data.TargetProvider
		case sortHealth:
			ih := healthRank(healthValue(items[i].Data.HealthStatus))
			jh := healthRank(healthValue(items[j].Data.HealthStatus))
			if ih != jh {
				return ih < jh
			}
			return items[i].Name < items[j].Name
		default:
			return items[i].Name < items[j].Name
		}
	})
}

func groupItems(items []pages.ProxyViewItem) []pages.DashboardGroup {
	groups := make(map[string][]pages.ProxyViewItem)
	order := make([]string, 0)

	for _, item := range items {
		cat := strings.TrimSpace(item.Category)
		if cat == "" {
			cat = "Ungrouped"
		}
		if _, exists := groups[cat]; !exists {
			order = append(order, cat)
		}
		groups[cat] = append(groups[cat], item)
	}

	result := make([]pages.DashboardGroup, 0, len(order))
	for _, cat := range order {
		result = append(result, pages.DashboardGroup{
			Name:  cat,
			Items: groups[cat],
		})
	}
	return result
}
