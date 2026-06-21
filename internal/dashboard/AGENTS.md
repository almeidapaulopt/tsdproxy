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
| `/stream/{name}/logs` | GET | `logStreamHandler` | admin |
| `/api/dashboard/preferences` | PUT | `updatePreferencesHandler` | viewer |
| `/api/dashboard/pin/{name}` | POST | `togglePinHandler` | viewer |
| `/api/proxy/{name}/restart` | POST | `restartHandler` | admin |
| `/api/proxy/{name}/pause` | POST | `pauseHandler` | admin |
| `/api/proxy/{name}/resume` | POST | `resumeHandler` | admin |
| `/api/proxy/{name}/reauth` | POST | `reauthHandler` | admin |

## SSE STREAMING

`streamProxyUpdates()` — long-running goroutine started in `Start()` (not the constructor):
1. Subscribes to `ProxyManager.SubscribeStatusEvents()` 
2. On each status event: renders HTML fragment via templ
3. Broadcasts to all connected SSE clients (`sseClients` map)

`logStreamer` — per-proxy log tail:
1. Subscribes to proxy's `LogBuffer.Subscribe()`
2. Batches log lines (drains every 100ms or on flush)
3. Sends append/remove/clear SSE events to client

## PREFERENCES

Identity key: `dashboardSubject(r)` (see GOTCHAS for full cascade).

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

All use `hx-post` with `hx-target="#actions-panel-{SafeID(name)}"` and `hx-swap="outerHTML"` — handler renders `ActionsPanel(...)` directly into the panel. SSE then independently broadcasts card/modal-badge updates as status propagates:
- **restart**: `pm.RestartProxy()` — stop then start
- **pause**: `pm.PauseProxy()` — stops proxy, keeps config
- **resume**: `pm.ResumeProxy()` — restarts from paused state
- **reauth**: cleans Tailscale auth state, triggers restart

Note: `hx-swap="none"` IS used elsewhere (e.g. dashboard.templ lines ~202, ~348) for cases where only SSE drives refresh, but NOT for proxy actions.

## GOTCHAS

- **SSE goroutine starts in `Start()` not constructor**: `go dash.streamProxyUpdates()` fires in `Dashboard.Start()`, after `NewDashboard()` returns. Constructor only sets up the struct; routes register via `AddRoutes()` separately.
- **`sseClients` map not thread-safe**: Protected by `d.mtx` mutex. All access must hold the lock.
- **Proxy action errors return JSON**: `writeJSONError()` used for action failures, not SSE events. Frontend must handle both SSE and JSON error responses.
- **HTMX header check**: `hxRequestHeader` constant checks for `HX-Request` header to distinguish HTMX from direct browser requests.
- **Identity resolution cascade**: `dashboardSubject(r)` returns `ResolveWhois(r).ID` → `__localhost__` (trusted source) → `__apikey__` (valid API key) → `__remote__` (rejected, prefs forbidden).
