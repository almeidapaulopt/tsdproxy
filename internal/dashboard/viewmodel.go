// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package dashboard

import (
	"sort"
	"strings"

	"github.com/almeidapaulopt/tsdproxy/internal/model"
	"github.com/almeidapaulopt/tsdproxy/internal/proxymanager"
)

var healthOrder = map[string]int{"healthy": 0, "unknown": 1, "down": 2}

func healthRank(h string) int {
	if r, ok := healthOrder[h]; ok {
		return r
	}
	return len(healthOrder)
}

type DashboardView struct {
	Proxies []ProxyViewItem
	Groups  []ProxyGroup
}

type ProxyViewItem struct {
	Name     string
	Category string
	Proxy    *proxymanager.Proxy
}

type ProxyGroup struct {
	Name  string
	Items []ProxyViewItem
}

func BuildDashboardView(
	proxies proxymanager.ProxyList,
	prefs model.Preferences,
	search string,
) DashboardView {
	pinnedSet := make(map[string]bool, len(prefs.Pinned))
	for _, p := range prefs.Pinned {
		pinnedSet[p] = true
	}

	var items []ProxyViewItem
	for name, p := range proxies {
		if !p.Config.Dashboard.Visible {
			continue
		}
		item := ProxyViewItem{
			Name:     name,
			Category: p.Config.Dashboard.Category,
			Proxy:    p,
		}
		if !matchesFilter(item, prefs, search) {
			continue
		}
		items = append(items, item)
	}

	sortItems(items, prefs.Sort, pinnedSet)

	view := DashboardView{Proxies: items}

	if prefs.Grouped {
		view.Groups = groupItems(items)
	}

	return view
}

func matchesFilter(item ProxyViewItem, prefs model.Preferences, search string) bool {
	if prefs.FilterStatus != "all" {
		status := item.Proxy.GetStatus()
		if status.String() != prefs.FilterStatus {
			return false
		}
	}

	if prefs.FilterHealth != "all" {
		health := healthValue(item.Proxy.GetHealth().Status.String())
		if health != prefs.FilterHealth {
			return false
		}
	}

	if search != "" {
		label := item.Proxy.Config.Dashboard.Label
		if label == "" {
			label = item.Name
		}
		if !strings.Contains(strings.ToLower(label), strings.ToLower(search)) {
			return false
		}
	}

	return true
}

func healthValue(h string) string {
	if h == "" {
		return "unknown"
	}
	return h
}

func sortItems(items []ProxyViewItem, sortKey string, pinned map[string]bool) {
	sort.SliceStable(items, func(i, j int) bool {
		iPinned := pinned[items[i].Name]
		jPinned := pinned[items[j].Name]
		if iPinned != jPinned {
			return iPinned
		}

		switch sortKey {
		case "status":
			si := items[i].Proxy.GetStatus()
			sj := items[j].Proxy.GetStatus()
			return si.String() < sj.String()
		case "provider":
			return items[i].Proxy.Config.TargetProvider < items[j].Proxy.Config.TargetProvider
		case "health":
			hi := items[i].Proxy.GetHealth()
			hj := items[j].Proxy.GetHealth()
			ih := healthRank(healthValue(hi.Status.String()))
			jh := healthRank(healthValue(hj.Status.String()))
			if ih != jh {
				return ih < jh
			}
			return items[i].Name < items[j].Name
		default:
			return items[i].Name < items[j].Name
		}
	})
}

func groupItems(items []ProxyViewItem) []ProxyGroup {
	groups := make(map[string][]ProxyViewItem)
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

	result := make([]ProxyGroup, 0, len(order))
	for _, cat := range order {
		result = append(result, ProxyGroup{
			Name:  cat,
			Items: groups[cat],
		})
	}
	return result
}
