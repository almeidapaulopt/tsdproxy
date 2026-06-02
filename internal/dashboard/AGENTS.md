# internal/dashboard

SSE-powered real-time dashboard: streaming proxy updates, log viewer, user preferences, proxy actions (restart/pause/resume/reauth).

## STRUCTURE

| File | Role |
|------|------|
| `dash.go` | `Dashboard` struct. Route registration, proxy action handlers, preference management, viewmodel construction. |
| `sse.go` | Server-Sent Events helpers: `writeSSEEvent`, `writeSSEData`, `writeSSEFlush`. Uses `//nolint:gosec` for templ-rendered data. |
| `stream.go` | SSE stream goroutine: subscribes to ProxyManager status events, broadcasts HTML fragments to all connected clients. `logStreamer` for per-proxy log tailing. |
| `preferences.go` | `PreferencesStore` — per-user JSON files at `{DataDir}/dashboard/preferences/{userID}.json`. Schema: dark, view, sort, grouped, filterStatus, filterHealth, pinned. |
| `sse_templ.go` | Generated templ file for SSE HTML fragments (DO NOT EDIT — auto-generated). |

## ROUTES (registered in `AddRoutes`)

| Pattern | Method | Handler | Auth |
|---------|--------|---------|------|
| `/` | GET | `dashboardHandler` | viewer |
| `/dashboard/list` | GET | `listFragmentHandler` | viewer |
| `/dashboard/proxy/{name}/modal` | GET | `proxyModalHandler` | viewer |
| `/stream` | GET | SSE stream | viewer |
| `/stream/{name}/logs` | GET | `logStreamHandler` | viewer |
| `/api/dashboard/preferences` | POST | `updatePreferencesHandler` | viewer |
| `/api/dashboard/pin/{name}` | POST | `togglePinHandler` | viewer |
| `/api/proxy/{name}/restart` | POST | `restartHandler` | admin |
| `/api/proxy/{name}/pause` | POST | `pauseHandler` | admin |
| `/api/proxy/{name}/resume` | POST | `resumeHandler` | admin |
| `/api/proxy/{name}/reauth` | POST | `reauthHandler` | admin |

## SSE STREAMING

`streamProxyUpdates()` — long-running goroutine started in `NewDashboard()` constructor:
1. Subscribes to `ProxyManager.SubscribeStatusEvents()` 
2. On each status event: renders HTML fragment via templ
3. Broadcasts to all connected SSE clients (`sseClients` map)

`logStreamer` — per-proxy log tail:
1. Subscribes to proxy's `LogBuffer.Subscribe()`
2. Batches log lines (drains every 100ms or on flush)
3. Sends append/remove/clear SSE events to client

## PREFERENCES

Identity resolution: `ResolveWhois(r).ID` → fallback `__localhost__`.

Persisted as JSON. Schema:
```go
type Preferences struct {
    Dark         bool     `json:"dark"`
    View         string   `json:"view"`         // "grid" | "list"
    Sort         string   `json:"sort"`         // "name" | "status" | "health"
    Grouped      bool     `json:"grouped"`
    FilterStatus []string `json:"filterStatus"`
    FilterHealth []string `json:"filterHealth"`
    Pinned       []string `json:"pinned"`       // proxy IDs
}
```

## VIEWMODEL

`buildDashboardViewData()` constructs the template data:
- Proxy list with status, health, ports, category, icon
- Sorted/filtered/grouped server-side based on preferences
- Health status formatted via `formatHealthStatus()`
- Port entries via `buildPortEntries()`

## PROXY ACTIONS

All use `hx-post` with `hx-swap="none"` — SSE drives the UI update:
- **restart**: `pm.RestartProxy()` — stop then start
- **pause**: `pm.PauseProxy()` — stops proxy, keeps config
- **resume**: `pm.ResumeProxy()` — restarts from paused state
- **reauth**: cleans Tailscale auth state, triggers restart

## GOTCHAS

- **SSE goroutine starts in constructor**: `go dash.streamProxyUpdates()` fires in `NewDashboard()` before routes are registered. Early events may be missed.
- **`sseClients` map not thread-safe**: Protected by `d.mtx` mutex. All access must hold the lock.
- **Proxy action errors return JSON**: `writeJSONError()` used for action failures, not SSE events. Frontend must handle both SSE and JSON error responses.
- **`_ = templ.Render()`**: Some render errors silently discarded (anti-pattern noted in root AGENTS.md).
- **HTMX header check**: `hxRequestHeader` constant checks for `HX-Request` header to distinguish HTMX from direct browser requests.
