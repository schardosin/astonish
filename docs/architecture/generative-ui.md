# Generative UI (Visual Apps)

## Overview

Generative UI brings in-chat UI generation to Astonish -- the AI writes React components in response to user requests, renders them live in a sandboxed iframe, and supports iterative refinement through conversation. This is similar to what Claude Artifacts, ChatGPT Canvas, and Vercel v0 provide: the user describes a UI ("build me a sales dashboard"), the AI generates a working React component, and it renders immediately in the chat. The user can then refine it ("add a search bar", "make the chart a bar chart") through follow-up messages.

Beyond ephemeral chat previews, generated UIs can be saved as **Apps** -- a new top-level concept alongside Flows, Fleet, and Drill. Saved apps persist as YAML definitions in `~/.config/astonish/apps/`, appear in a dedicated Apps tab in Studio, and can fetch live data through the Go backend proxy (leveraging existing MCP tools and HTTP APIs). This creates a natural progression: prototype in chat, refine interactively, save as a reusable app, connect to live data.

## Key Design Decisions

### Why LLM-Generated JSX Over a Component Schema

Two approaches were considered:

1. **Structured JSON schema → predefined components**: The AI outputs JSON that maps to a fixed component library (tables, charts, forms). Safer, more predictable, but limited to what the library supports.
2. **LLM generates React JSX**: The AI writes actual React code rendered in a sandboxed iframe. Unlimited flexibility -- any UI the AI can imagine.

JSX generation was chosen because:

- The expressiveness ceiling is dramatically higher. A component schema can handle dashboards and forms but fails for creative UIs (games, visualizations, interactive tutorials, custom layouts).
- Modern LLMs are excellent at writing React code. Claude, GPT-4, and similar models produce working React components reliably.
- The sandboxed iframe provides security isolation equivalent to what Claude Artifacts uses -- the generated code cannot access the parent page, cookies, or local storage.
- The same code that renders in the preview can be saved directly as the app source. No translation layer between "preview format" and "storage format."

The tradeoff is that generated code can crash. This is mitigated by an error boundary inside the iframe that catches runtime errors and reports them to the parent, allowing the AI to fix the issue in the refinement loop.

### Why a Sandboxed Iframe with Sucrase

The generated JSX needs a runtime environment. Three options were evaluated:

- **Full Vite dev server per app**: Most compatible but heavyweight. Each preview would spawn a Node.js process, use ~100MB RAM, and take seconds to start.
- **Incus container sandbox**: Maximum isolation but requires the sandbox infrastructure. Too heavy for a UI preview that just needs to render React.
- **Sucrase + iframe sandbox**: Sucrase is a 50KB JSX-to-JS compiler that runs in the browser. Combined with an iframe using `sandbox="allow-scripts"`, it provides fast compilation (~5ms) with strong isolation.

Sucrase was chosen because:

- It compiles JSX to `React.createElement` calls without a full Babel toolchain. No AST plugins, no module resolution -- just JSX transformation.
- The iframe's `sandbox` attribute restricts the generated code: no access to parent DOM, no `fetch` to arbitrary origins (communication goes through `postMessage`), no navigation, no popups.
- The `srcdoc` attribute embeds the entire runtime as a self-contained HTML document, avoiding any network dependency for the preview itself.
- Cold start is effectively zero -- Sucrase compiles on the main thread in single-digit milliseconds for typical component sizes.

Libraries (React, Tailwind, Lucide, Recharts) are loaded from CDN in the iframe's HTML template. This avoids bundling them into the main Astonish UI bundle while ensuring they're cached across preview renders.

### Why a New "Apps" Concept Instead of Extending Flows

Flows are YAML-defined node graphs executed by the AstonishAgent state machine. They model sequential processing: input → LLM → tool → conditional → output. Visual apps are fundamentally different:

- **Flows are backend execution graphs.** Apps are frontend React components.
- **Flows have nodes and edges.** Apps have JSX source code and data sources.
- **Flows run once per invocation.** Apps are persistent, interactive UIs.
- **Flows use `{{variable}}` interpolation.** Apps use React state and hooks.

Forcing apps into the flow schema would require either (a) a special "render JSX" node type that doesn't fit the state machine model, or (b) ignoring most of the flow infrastructure. A separate `~/.config/astonish/apps/` directory with its own YAML schema is cleaner, avoids confusion in the flow listing, and allows app-specific features (data sources, polling intervals, theming) without polluting the flow schema.

The Apps tab sits alongside Chat, Flows, Fleet, and Drill in the top navigation, establishing apps as a first-class concept.

### Why Backend-Proxied Data Fetching

Apps need live data -- database queries, API calls, MCP tool invocations. Three options were considered:

1. **Direct fetch from iframe**: The generated code calls `fetch()` directly. Simple but fundamentally broken: the iframe sandbox restricts network access, CORS blocks most APIs, and there's no way to inject authentication tokens securely.
2. **Proxy through Go backend**: The iframe sends data requests via `postMessage` to the parent page, which calls a Go API endpoint, which executes the actual request. Results flow back through the same channel.
3. **Hybrid**: Backend proxy for MCP/tool data, direct fetch for public APIs.

Backend proxy was chosen because:

- **Security**: API keys and credentials never reach the iframe. The Go backend uses the existing credential system (`{{CREDENTIAL:name:field}}` substitution) to inject secrets server-side.
- **MCP integration**: The proxy can invoke any registered MCP tool, giving apps access to databases, GitHub, Slack, and every other MCP server configured in Astonish -- without the iframe needing to know anything about MCP.
- **CORS elimination**: The Go backend makes server-side HTTP requests, bypassing browser CORS restrictions entirely.
- **Audit trail**: All data requests go through the backend, providing a single point for logging, rate limiting, and access control.

The tradeoff is latency (iframe → postMessage → HTTP → backend → data source → response chain), but for dashboard-style polling this is negligible. Real-time use cases use SSE streaming from the backend, forwarded to the iframe via `postMessage`.

### Why Chat-First Iterative Refinement

The initial implementation prioritizes the chat experience over the Apps infrastructure because:

- The chat preview is the core innovation. Without it, the Apps tab is just a file manager.
- Iterative refinement validates the approach immediately. If the AI can generate, render, and refine a component through conversation, everything else (saving, data sources, management) is incremental.
- The distillation system provides a proven pattern: preview in chat → save to disk. The app save flow follows the same UX as `/distill` for flows.

## Architecture

### Phase 1: In-Chat JSX Preview

The foundation: user requests a UI in chat, the AI generates JSX, and it renders inline as a live preview.

```
User: "Build me a calculator app"
  |
  v
ChatAgent: detects UI generation intent
  |
  v
LLM generates JSX with ```astonish-app fence marker
  |
  v
SSE stream: text events contain the fenced code block
  |
  v
StudioChat.tsx: detects ```astonish-app in streaming text
  |
  v
AppPreview.tsx: renders in sandboxed iframe
  |
  +-- Sucrase compiles JSX → JS
  +-- React 19 renders the component
  +-- Tailwind CSS styles it
  +-- Error boundary catches crashes
  |
  v
User sees a working calculator in the chat
```

#### Iframe Sandbox Runtime

The `AppPreview` component constructs an iframe with a self-contained HTML document via `srcdoc`:

```html
<!DOCTYPE html>
<html>
<head>
  <script src="https://cdn.jsdelivr.net/npm/react@19/umd/react.production.min.js"></script>
  <script src="https://cdn.jsdelivr.net/npm/react-dom@19/umd/react-dom.production.min.js"></script>
  <script src="https://cdn.jsdelivr.net/npm/sucrase@3/dist/sucrase.js"></script>
  <script src="https://cdn.jsdelivr.net/npm/recharts@2/umd/Recharts.js"></script>
  <link href="https://cdn.jsdelivr.net/npm/@tailwindcss/cdn@4" rel="stylesheet">
  <!-- Lucide icons injected as a global object -->
</head>
<body>
  <div id="root"></div>
  <script>
    window.addEventListener('message', (e) => {
      if (e.data.type === 'render') {
        try {
          const code = Sucrase.transform(e.data.code, {
            transforms: ['jsx'],
            jsxPragma: 'React.createElement',
            jsxFragmentPragma: 'React.Fragment'
          }).code;
          const module = {};
          new Function('React', 'module', 'exports', ...libs, code)(
            React, module, module, ...libValues
          );
          const Component = module.exports.default || module.exports;
          ReactDOM.createRoot(document.getElementById('root')).render(
            React.createElement(Component)
          );
          window.parent.postMessage({ type: 'render_success', height: document.body.scrollHeight }, '*');
        } catch (err) {
          window.parent.postMessage({ type: 'render_error', error: err.message, stack: err.stack }, '*');
        }
      }
    });
  </script>
</body>
</html>
```

The iframe uses these sandbox attributes:
- `sandbox="allow-scripts"`: Permits JavaScript execution but blocks top-navigation, popups, form submission, and same-origin access.
- No `allow-same-origin`: The iframe cannot access the parent page's DOM, cookies, localStorage, or session data.

#### SSE Event: `app_preview`

A new SSE event type carries the generated app to the frontend:

```
event: app_preview
data: {"code":"function Calculator() { ... }","title":"Calculator","description":"A basic calculator","version":1}
```

Backend struct:

```go
type AppPreviewData struct {
    Code        string `json:"code"`
    Title       string `json:"title"`
    Description string `json:"description"`
    Version     int    `json:"version"`
}
```

The event is emitted when the backend detects an `astonish-app` fenced code block in the LLM's streaming response. Detection happens in the SSE text handler: accumulated text is scanned for the fence markers, and when a complete block is found, the `app_preview` event is sent alongside the text event.

#### Frontend Message Type

```typescript
interface AppPreviewMessage {
    type: 'app_preview'
    code: string
    title: string
    description: string
    version: number
}
```

This is added to the `ChatMsg` union in `chatTypes.ts`. In `StudioChat.tsx`, the `app_preview` SSE event handler creates an `AppPreviewMessage` and appends it to the message list. The message renderer displays the `AppPreview` component with a toolbar: **Open Fullscreen**, **View Code**, **Save as App**.

#### Code Fence Detection

The LLM is instructed (via system prompt injection) to wrap generated UI code in a specific fence:

````
```astonish-app
function MyComponent() {
  const [count, setCount] = React.useState(0);
  return (
    <div className="p-4">
      <button onClick={() => setCount(c => c + 1)}>Count: {count}</button>
    </div>
  );
}
```
````

The frontend detects this fence in the streaming text and:
1. Extracts the code content.
2. Creates an `AppPreviewMessage` and inserts it into the message list.
3. Hides the raw code block from the markdown rendering (replaces it with a reference to the rendered preview).

This dual-path approach (code fence in text + explicit SSE event) ensures the preview works whether the AI uses a tool or outputs inline code.

#### System Prompt for UI Generation

When the AI detects a UI generation intent (via the existing intent classification system or an explicit `/app` command), a supplementary system prompt is injected:

```
You can generate interactive React components that render live in the user's browser.

When creating a UI component, wrap your code in an ```astonish-app fence. Rules:
- Write a single default-exported React function component
- React is available globally (use React.useState, React.useEffect, etc.)
- Tailwind CSS v4 is available for styling
- Lucide icons: import from the global `icons` object (e.g., icons.Search, icons.Plus)
- Recharts: BarChart, LineChart, PieChart, etc. are available globally
- Do NOT use import statements -- all libraries are pre-loaded
- Use React.useState and React.useEffect for state and side effects
- The component should be self-contained and immediately renderable
```

#### Preview UX in Chat

The app preview renders in the chat as an elevated card:

```
┌─────────────────────────────────────────────┐
│  ◆ Calculator                    [⛶] [</>] [💾] │
│─────────────────────────────────────────────│
│                                             │
│        ┌──────────────────┐                │
│        │           7 8 9  │                │
│        │  Display   4 5 6 │                │
│        │           1 2 3  │                │
│        │             0 .  │                │
│        └──────────────────┘                │
│                                             │
└─────────────────────────────────────────────┘
```

Toolbar actions:
- **⛶ Fullscreen**: Opens the iframe in a modal overlay filling the viewport.
- **</> Code**: Toggles a CodeMirror panel showing the JSX source.
- **💾 Save**: Triggers the save-as-app flow (Phase 3).

The preview card auto-sizes to the iframe's content height (communicated via `postMessage` from the iframe after render). Maximum height in the chat is 500px with scroll; fullscreen removes this limit.

### Phase 2: Iterative Refinement

The user can modify the generated app through conversation, with each iteration updating the live preview.

```
User: "Make the header blue and add a search bar"
  |
  v
ChatAgent: detects active app context
  |
  v
System prompt includes current JSX source as context:
  "The user is refining an app. Current source code:
   ```
   function Dashboard() { ... }
   ```
   Apply the requested changes and output the updated component."
  |
  v
LLM generates updated JSX
  |
  v
Frontend: updates the existing AppPreviewMessage (same card, new version)
  |
  v
Iframe re-renders with new code (smooth transition)
```

#### Active App Tracking

The chat session maintains state for the "active app" being refined:

```go
type ActiveApp struct {
    Code     string   `json:"code"`
    Title    string   `json:"title"`
    Versions []string `json:"versions"` // history of code versions
    Version  int      `json:"version"`
}
```

This is stored in the ChatAgent's per-session state (similar to `pendingDistillReview`). When the user sends a follow-up message while an app is active, the current source code is injected into the LLM context so it can apply incremental changes.

#### Version History

Each refinement increments the version counter and stores the previous code in the versions array. The preview card shows version navigation:

```
┌─────────────────────────────────────────────┐
│  ◆ Dashboard  v3          [◀ v2] [v4 ▶]    │
│─────────────────────────────────────────────│
│  (current version of the app)               │
└─────────────────────────────────────────────┘
```

Users can browse previous versions. Clicking a previous version restores that code in the preview. Sending a new message from an older version creates a branch (the new version is based on the displayed version, not the latest).

#### Session Persistence

App previews are persisted to the session JSONL transcript using the same prefix-marker pattern as distill previews:

```
[app_preview]{"code":"...","title":"...","description":"...","version":3}
```

On session reload, `tryParseAppPreviewMessage()` detects these prefixes and reconstructs `AppPreviewMessage` objects. The version history is reconstructed by collecting all `app_preview` entries with the same title.

#### Refinement Intent Detection

When a pending active app exists and the user sends a message, the system must determine if the message is:

1. **App refinement**: "Make the header blue" → update the app.
2. **App action**: "Save this app" → trigger save flow.
3. **Unrelated**: "What's the weather?" → normal chat, deactivate the app context.

This uses a lightweight LLM classification call (similar to `ClassifyDistillReviewIntent()`) or keyword heuristics for obvious cases ("save", "done", "cancel").

### Phase 3: Apps Persistence & Management

Users can save generated UIs as Apps and manage them from a dedicated tab.

#### App YAML Schema

```yaml
name: sales_dashboard
description: Real-time sales metrics dashboard with filtering
version: 3
created_at: 2026-04-20T10:00:00Z
updated_at: 2026-04-20T10:30:00Z
session_id: abc-123-def-456  # originating chat session

code: |
  function SalesDashboard() {
    const [data, setData] = React.useState([]);
    const [filter, setFilter] = React.useState('all');
    // ... full JSX component source
  }

data_sources:
  - id: sales_data
    type: mcp_tool
    config:
      server: postgres-mcp
      tool: query
      args:
        query: "SELECT * FROM sales ORDER BY date DESC LIMIT 100"
    interval: 30s

  - id: exchange_rates
    type: http_api
    config:
      url: "https://api.exchangerate-api.com/v4/latest/USD"
      method: GET
    interval: 60s
```

#### Go Structs

```go
// pkg/apps/types.go
type VisualApp struct {
    Name        string       `json:"name" yaml:"name"`
    Description string       `json:"description" yaml:"description"`
    Code        string       `json:"code" yaml:"code"`
    Version     int          `json:"version" yaml:"version"`
    DataSources []DataSource `json:"dataSources,omitempty" yaml:"data_sources,omitempty"`
    CreatedAt   time.Time    `json:"createdAt" yaml:"created_at"`
    UpdatedAt   time.Time    `json:"updatedAt" yaml:"updated_at"`
    SessionID   string       `json:"sessionId,omitempty" yaml:"session_id,omitempty"`
}

type DataSource struct {
    ID       string         `json:"id" yaml:"id"`
    Type     string         `json:"type" yaml:"type"`     // "mcp_tool", "http_api", "static"
    Config   map[string]any `json:"config" yaml:"config"`
    Interval string         `json:"interval,omitempty" yaml:"interval,omitempty"`
}
```

Storage location: `~/.config/astonish/apps/{app_name}.yaml`

#### API Endpoints

| Method | Endpoint | Purpose |
|---|---|---|
| `GET` | `/api/apps` | List all saved apps (name, description, updatedAt) |
| `GET` | `/api/apps/:name` | Get full app definition including code |
| `PUT` | `/api/apps/:name` | Create or update an app |
| `DELETE` | `/api/apps/:name` | Delete an app |
| `POST` | `/api/apps/:name/data` | Proxy a data request for a running app |

These are registered in `pkg/api/server.go` alongside the existing route groups.

#### Save Flow (Chat → App)

When the user clicks "Save as App" or says "save this app":

1. A save dialog appears (name, description -- pre-filled from the AI's suggestions).
2. The frontend calls `PUT /api/apps/:name` with the current code, title, description, data sources, and originating session ID.
3. The backend writes the YAML to `~/.config/astonish/apps/{name}.yaml`.
4. An `app_saved` SSE event confirms the save.
5. A DOM event (`astonish:apps-updated`) refreshes the Apps tab listing.

This mirrors the distill save flow almost exactly: preview in chat → confirm → save to disk → notification.

#### Apps Tab (Frontend)

A new top-level tab in `TopBar.tsx`:

```
[Chat] [Flows] [Apps] [Fleet] [Drill]
```

The Apps view (`AppsView.tsx`) has two sub-views:

**List View:**
- Grid of saved apps with name, description, last updated
- Search and filter
- Click to run, edit, or delete

**Runner View:**
- Full-viewport iframe rendering of the app
- Toolbar: "Edit in Chat" (opens a new chat session with the app's code as context), "View Code", "Data Sources", "Settings"
- Data sources are active (polling or streaming via the backend proxy)

### Phase 4: Live Data & Backend Proxy

Apps fetch data through the Go backend, with support for MCP tools, HTTP APIs, and real-time updates.

#### Data Request Flow

```
App (iframe)
  |
  | postMessage({ type: 'data_request', sourceId: 'sales_data', requestId: 'req-1' })
  |
  v
Parent page (AppPreview.tsx)
  |
  | POST /api/apps/:name/data { sourceId: 'sales_data', requestId: 'req-1' }
  |
  v
Go backend (app_data_handler.go)
  |
  +-- type: mcp_tool → invoke MCP tool via existing infrastructure
  |     - Look up server from config
  |     - Call tool with args
  |     - Return result as JSON
  |
  +-- type: http_api → server-side HTTP request
  |     - Apply credential substitution to URL/headers
  |     - Make request, return response body
  |
  v
Response flows back: backend → HTTP → parent → postMessage → iframe
```

#### postMessage Protocol

Messages between the iframe and parent page use a typed protocol:

**Iframe → Parent:**

| Message Type | Fields | Purpose |
|---|---|---|
| `render_success` | `height` | Component rendered, report content height |
| `render_error` | `error`, `stack` | Compilation or runtime error |
| `data_request` | `sourceId`, `requestId`, `args` | Request data from a source |
| `action_request` | `actionId`, `requestId`, `payload` | Trigger a backend action |

**Parent → Iframe:**

| Message Type | Fields | Purpose |
|---|---|---|
| `render` | `code` | Send JSX source to compile and render |
| `data_response` | `requestId`, `data`, `error` | Response to a data request |
| `data_update` | `sourceId`, `data` | Pushed data update (from polling/SSE) |
| `theme` | `mode` | Light/dark theme sync |

#### Pre-Injected Hooks

The iframe runtime includes pre-built hooks that abstract the postMessage protocol:

```javascript
// Available inside generated apps without imports

function useAppData(sourceId, options = {}) {
  const [data, setData] = React.useState(null);
  const [loading, setLoading] = React.useState(true);
  const [error, setError] = React.useState(null);

  React.useEffect(() => {
    // Sends postMessage to parent
    // Parent proxies to backend
    // Handles polling based on DataSource.interval
    // Returns { data, loading, error, refetch }
  }, [sourceId]);

  return { data, loading, error, refetch };
}

function useAppAction(actionId) {
  // Returns an async function that sends an action_request
  // via postMessage, waits for the response, and returns the result
  return React.useCallback(async (payload) => {
    // postMessage → parent → backend → response
  }, [actionId]);
}
```

The AI is instructed to use these hooks when generating apps that need live data:

```jsx
function SalesDashboard() {
  const { data, loading } = useAppData('sales_data');

  if (loading) return <div>Loading...</div>;

  return (
    <Recharts.BarChart data={data}>
      <Recharts.Bar dataKey="revenue" fill="#8884d8" />
    </Recharts.BarChart>
  );
}
```

#### Polling and Streaming

Data sources with an `interval` field are automatically polled:

- The parent page sets up a `setInterval` that calls the data proxy endpoint.
- Results are forwarded to the iframe via `data_update` postMessages.
- The `useAppData` hook receives these updates and triggers React re-renders.

For real-time data sources, the parent establishes an SSE connection to `GET /api/apps/:name/stream?sourceId=X`. Events from this stream are forwarded to the iframe as `data_update` messages.

#### Data Source Configuration UI

When saving an app that uses `useAppData`, the save dialog includes a data source configuration section:

```
┌─────────────────────────────────────────────────────┐
│  Data Source: sales_data                            │
│                                                     │
│  Type: [MCP Tool ▾]                                │
│  Server: [postgres-mcp ▾]                          │
│  Tool: [query ▾]                                   │
│  Args:                                             │
│    query: SELECT * FROM sales ORDER BY date DESC   │
│  Refresh: [Every 30 seconds ▾]                     │
└─────────────────────────────────────────────────────┘
```

The AI can also define data sources inline when generating the app, and the save flow extracts them from the code annotations.

### Phase 5: Polish & Advanced Features

#### App Templates

Pre-built starting points for common patterns:

| Template | Description | Data Sources |
|---|---|---|
| Dashboard | Metric cards, charts, filters | Configurable |
| CRUD Table | Sortable table with create/edit/delete | MCP or HTTP |
| Form Builder | Multi-step form with validation | Action triggers |
| Data Viewer | JSON/CSV explorer with search | File or API |
| Monitoring | Real-time metrics with alerts | Streaming |

Templates are stored as YAML files in the official store and can be loaded via the `/app` command:

```
/app template:dashboard
```

#### App Export

Saved apps can be exported as:

- **Standalone HTML**: A single file containing the React runtime, Tailwind, and the app code. Works offline, shareable.
- **YAML definition**: For sharing between Astonish instances or publishing to a store.

#### Multi-View Apps

Support for apps with multiple pages/views:

```yaml
code: |
  function App() {
    const [view, setView] = React.useState('dashboard');
    return (
      <div>
        <nav>
          <button onClick={() => setView('dashboard')}>Dashboard</button>
          <button onClick={() => setView('settings')}>Settings</button>
        </nav>
        {view === 'dashboard' && <Dashboard />}
        {view === 'settings' && <Settings />}
      </div>
    );
  }
```

This is supported naturally by the JSX approach -- the AI generates a component with internal routing. No framework changes needed.

#### Code Editor

The Apps tab includes a built-in CodeMirror editor for manual code editing:

- Split view: editor on left, live preview on right
- Changes in the editor update the preview in real-time (debounced Sucrase compilation)
- Uses the same CodeMirror instance already used for YAML editing in the flow canvas

## Implementation Phases

| Phase | Description | Dependencies | New Files |
|---|---|---|---|
| **1** | In-chat JSX preview: iframe sandbox, Sucrase compilation, code fence detection, `app_preview` SSE event, `AppPreview` component | None (foundation) | `web/src/components/chat/AppPreview.tsx`, updates to `StudioChat.tsx`, `chatTypes.ts`, `chat_runner.go`, `chat_handlers.go` |
| **2** | Iterative refinement: active app tracking, version history, session persistence, refinement intent detection | Phase 1 | Updates to `chat_agent.go`, `chat_utils.go`, `StudioChat.tsx` |
| **3** | Apps persistence: YAML storage, CRUD API, Apps tab, save-from-chat flow | Phase 1 | `pkg/apps/` package, `pkg/api/app_handlers.go`, `web/src/components/AppsView.tsx`, `web/src/api/apps.ts` |
| **4** | Live data: backend proxy, postMessage protocol, `useAppData`/`useAppAction` hooks, polling/streaming | Phase 3 | `pkg/api/app_data_handler.go`, iframe runtime updates |
| **5** | Polish: templates, export, multi-view, code editor | Phase 3 | Various |

Phase 1 is the critical foundation. Everything else builds incrementally on top of it. The approach is designed so that each phase delivers usable functionality -- Phase 1 alone lets users generate and preview UIs in chat, which is valuable even without persistence or data fetching.

## Key Files (Planned)

| File | Purpose |
|---|---|
| `web/src/components/chat/AppPreview.tsx` | Sandboxed iframe component: Sucrase compilation, postMessage communication, error boundary, height auto-sizing |
| `web/src/components/chat/AppPreviewCard.tsx` | Chat message card wrapping AppPreview with toolbar (fullscreen, code view, save) and version navigation |
| `web/src/components/AppsView.tsx` | Apps tab: list view (grid of saved apps) and runner view (full-viewport iframe) |
| `web/src/api/apps.ts` | API client: fetchApps, fetchApp, saveApp, deleteApp, proxyDataRequest |
| `web/src/components/chat/chatTypes.ts` | New `AppPreviewMessage` type in the ChatMsg union |
| `pkg/apps/types.go` | Go types: VisualApp, DataSource |
| `pkg/apps/store.go` | App YAML storage: load, save, list, delete from `~/.config/astonish/apps/` |
| `pkg/api/app_handlers.go` | REST handlers: CRUD for apps, data proxy endpoint |
| `pkg/api/chat_runner.go` | App preview SSE event emission, code fence detection in streaming text |
| `pkg/agent/chat_agent.go` | ActiveApp state tracking for iterative refinement |
| `pkg/agent/chat_app.go` | App refinement logic: intent classification, context injection, version management |

## Interactions

- **Agent Engine**: The ChatAgent detects UI generation intent and injects a supplementary system prompt with component-writing instructions. The active app context is tracked per-session for refinement. The same `BeforeModelCallback` architecture injects the current source code when refining.
- **Sessions**: App previews are persisted in the session JSONL transcript using prefix markers (`[app_preview]`), following the same pattern as distill previews. Session reload reconstructs app preview messages and version history.
- **MCP**: The data proxy endpoint invokes MCP tools via the existing tool infrastructure. Any MCP server configured in Astonish is available as a data source for apps, providing access to databases, APIs, and external services.
- **Credentials**: Data source configurations support `{{CREDENTIAL:name:field}}` placeholders. The backend substitutes real values when proxying requests, so credentials never reach the iframe.
- **API & Studio**: New REST endpoints for app CRUD and data proxy. The Apps tab is a new top-level view in the Studio UI. The `TopBar` component gains an "Apps" navigation item.
- **Flows**: Apps and flows are independent concepts with separate storage and schemas. However, a flow could invoke an app (display it to the user) via a future `show_app` output node type, and an app could trigger a flow via the action system.
- **Sandbox**: The iframe sandbox is browser-native (not Incus). However, data sources that invoke MCP tools may execute within sandbox containers if the MCP server runs in a container (via the existing `ContainerMCPTransport`).
