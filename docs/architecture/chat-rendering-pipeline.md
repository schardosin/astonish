# Chat Rendering Pipeline

## Overview

The Studio Chat (`StudioChat.tsx`) is the primary user interface for Astonish. It orchestrates 17 sub-components, manages 35 state variables, handles 29 SSE event types from the backend, and renders 21 distinct message types. This document is the authoritative reference for how the pipeline works, how to extend it, and what rules must be followed.

**Target audience:** Developers adding new SSE event types, new rendering components, or modifying the report/app/artifact pipelines.

## End-to-End Data Flow

The rendering pipeline is a unidirectional flow from user input to rendered UI:

```mermaid
graph LR
    A[User Input] -->|POST /api/studio/chat| B[Go Backend<br/>chat_runner.go]
    B -->|SSE text/event-stream| C[connectChat<br/>SSE Parser]
    C -->|onEvent callback| D[StudioChat.tsx<br/>Event Handlers]
    D -->|setMessages / setState| E[React State]
    E -->|render loop| F[Sub-Components]
```

### Detailed pipeline:

```mermaid
sequenceDiagram
    participant User
    participant StudioChat
    participant connectChat
    participant Backend
    participant LLM

    User->>StudioChat: Types message, clicks Send
    StudioChat->>connectChat: POST /api/studio/chat {message, sessionId}
    connectChat->>Backend: HTTP POST (streaming response)
    Backend->>LLM: Forward prompt
    LLM-->>Backend: Stream tokens
    Backend-->>connectChat: SSE: event:text, data:{text:"Hello"}
    connectChat-->>StudioChat: onEvent("text", {text:"Hello"})
    StudioChat->>StudioChat: setMessages() — append/update agent message
    StudioChat->>StudioChat: React re-render → ReactMarkdown bubble

    Note over Backend,LLM: LLM decides to call a tool
    LLM-->>Backend: tool_call(write_file, {path, content})
    Backend-->>connectChat: SSE: event:tool_call
    Backend->>Backend: Execute tool, write file to disk
    Backend-->>connectChat: SSE: event:tool_result
    Backend-->>connectChat: SSE: event:artifact

    Note over Backend,LLM: LLM writes final summary
    LLM-->>Backend: Stream summary text
    Backend-->>connectChat: SSE: event:text (summary chunks)
    Backend-->>connectChat: SSE: event:done

    StudioChat->>StudioChat: isFinalResult heuristic → ResultCard
    StudioChat->>User: Rendered: ResultCard + EmbeddedFileViewer + Files button
```

## SSE Transport Layer

SSE parsing is implemented **manually** using the `ReadableStream` API in `web/src/api/studioChat.ts` -- not the browser's native `EventSource`. This is because the initial request is a `POST` (EventSource only supports GET).

Two functions share identical parsing logic:

| Function | HTTP Method | Endpoint | Purpose |
|----------|------------|----------|---------|
| `connectChat()` | POST | `/api/studio/chat` | Send a new message, receive streaming response |
| `connectChatStream()` | GET | `/api/studio/sessions/{id}/stream` | Reconnect to an active background runner |

**Parsing flow:**
1. Read chunks from `response.body.getReader()` via `TextDecoder`
2. Accumulate into a buffer, split on `\n\n` (SSE block delimiter)
3. For each block: extract `event:` type and `data:` payload from lines
4. `JSON.parse(data)` and call `onEvent(eventType, parsedData)`
5. On stream end, call `onDone()`

**Both connect paths must have identical event handlers.** When adding a new event type, the `case` must be added to both the `connectChat` handler (in `sendMessage`) and the `connectChatStream` handler (in the reconnect logic). Missing one causes events to be silently dropped on reconnect.

## SSE Event Types

The backend emits **27 distinct event types** across `chat_runner.go` and `chat_handlers.go`. The frontend handles **29 case entries** (some events like `app_done`/`app_saved` share a handler).

### Core Chat Events

| Event | Data Shape | Frontend Action |
|-------|-----------|----------------|
| `session` | `{sessionId, isNew}` | Store session ID, update sidebar |
| `text` | `{text}` | Append to streaming agent message |
| `done` | `{}` | Finalize streaming text, set `isStreaming=false` |
| `error` | `{error}` | Add error message |
| `error_info` | `{title, reason, suggestion, originalError}` | Add structured error card |
| `usage` | `{input, output, total}` | Update token usage display |

### Tool Execution Events

| Event | Data Shape | Frontend Action |
|-------|-----------|----------------|
| `tool_call` | `{name, args, id}` | Add tool call card (collapsible) |
| `tool_result` | `{name, result, id}` | Add tool result card (collapsible) |
| `approval` | `{name, args, options}` | Show approval card with Approve/Deny buttons |
| `auto_approved` | `{name}` | Show auto-approved badge |

### Artifact & File Events

| Event | Data Shape | Frontend Action |
|-------|-----------|----------------|
| `artifact` | `{path, toolName, fileName, fileType}` | Add to `sessionArtifacts`, show ArtifactCard |
| `image` | `{data, mimeType}` | Render inline `<img>` |

### App Preview Events

| Event | Data Shape | Frontend Action |
|-------|-----------|----------------|
| `app_preview` | `{code, title, description, version, appId}` | Finalize streaming text, render AppPreviewCard |
| `app_done` / `app_saved` | `{name, path}` | Clear active app, show saved confirmation |

### Delegation Events

| Event | Data Shape | Frontend Action |
|-------|-----------|----------------|
| `subtask_progress` | `{tasks[], events[]}` | Update TaskPlanPanel |
| `fleet_progress` | `{agents[], events[], status}` | Update FleetExecutionPanel |
| `fleet_redirect` | `{message}` | Open fleet dialog |
| `fleet_plan_redirect` | `{message}` | Open fleet template picker |
| `drill_redirect` | `{name, message}` | Switch to drill wizard |
| `drill_add_redirect` | `{message}` | Switch to drill-add wizard |

### Planning & Flow Events

| Event | Data Shape | Frontend Action |
|-------|-----------|----------------|
| `distill_preview` | `{yaml, flowName, description, tags, explanation}` | Render DistillPreviewCard |
| `distill_saved` | `{filePath, runCommand}` | Show flow-saved confirmation |
| `flow_output` | `{content}` | Render as agent message |

### Other Events

| Event | Data Shape | Frontend Action |
|-------|-----------|----------------|
| `thinking` | `{text}` | Show thinking indicator |
| `retry` | `{attempt, maxRetries, reason}` | Show retry badge |
| `session_title` | `{title}` | Update sidebar session title |
| `new_session` | `{sessionId}` | Switch to new session |
| `system` | `{text}` | Show system info card |

## Message Types and Component Mapping

Each SSE event handler calls `setMessages()` to add a typed message to the React state. The render loop maps each message type to a component:

```mermaid
graph TD
    subgraph "SSE Events (Backend)"
        E1[text] --> M1[agent]
        E2[tool_call] --> M2[tool_call]
        E3[tool_result] --> M3[tool_result]
        E4[artifact] --> M4[artifact]
        E5[app_preview] --> M5[app_preview]
        E6[error] --> M6[error]
        E7[approval] --> M7[approval]
        E8[subtask_progress] --> M8[subtask_execution]
    end

    subgraph "Message Types (React State)"
        M1 --> C1["ResultCard (isFinalResult)<br/>or ReactMarkdown bubble"]
        M2 --> C2[renderToolCard]
        M3 --> C2
        M4 --> C3[ArtifactCard]
        M5 --> C4[AppPreviewCard]
        M6 --> C5[Error div]
        M7 --> C6[Approval card]
        M8 --> C7[TaskPlanPanel]
    end
```

### Full type-to-component table

| Message Type | Component | Notes |
|-------------|-----------|-------|
| `user` | Inline chat bubble | "You" label, plain text |
| `agent` | `ReactMarkdown` bubble **or** `ResultCard` | `isFinalResult` heuristic selects ResultCard |
| `tool_call` | `renderToolCard()` | Collapsible tool invocation card |
| `tool_result` | `renderToolCard()` | Collapsible tool response card |
| `browser_handoff` | `BrowserView` | VNC proxy + page info |
| `image` | Inline `<img>` | Base64 data URI |
| `error` | Red error `<div>` | Plain text |
| `error_info` | Structured error card | Title, reason, suggestion, raw error |
| `approval` | Yellow approval card | Approve/Deny action buttons |
| `auto_approved` | Green badge | Auto-approved notification |
| `thinking` | Thinking note | Italic indicator |
| `fleet_execution` | `FleetExecutionPanel` | Multi-agent progress |
| `plan` | `PlanPanel` | Goal + step checklist |
| `subtask_execution` | `TaskPlanPanel` | Delegation progress |
| `fleet_message` | Fleet chat bubble | Agent-colored markdown bubble |
| `system` | System info card | Info icon + markdown |
| `retry` | Orange retry badge | Attempt counter |
| `artifact` | `ArtifactCard` | File notification (suppressed when embedded in ResultCard) |
| `app_preview` | `AppPreviewCard` | Sandboxed iframe with live React preview |
| `distill_preview` | `DistillPreviewCard` | Flow YAML preview with save button |
| `distill_saved` | Saved confirmation card | File path + copy button |
| `app_saved` | Saved confirmation card | App name + path |

## The Report Pipeline

Reports are the most complex rendering path because they involve multiple events, a heuristic, and three export formats. This pipeline was carefully designed -- **do not change it without understanding all the moving parts**.

### How it works

```mermaid
sequenceDiagram
    participant LLM
    participant Backend
    participant Frontend
    participant FileSystem

    Note over LLM: System prompt says:<br/>"Use write_file for reports"

    LLM->>Backend: tool_call: write_file({path: "report.md", content: "# Full Report..."})
    Backend->>FileSystem: Write file to disk
    Backend->>Frontend: SSE: artifact {path, toolName: "write_file", fileName, fileType}
    Frontend->>Frontend: sessionArtifacts.push(artifact)<br/>Files button appears in toolbar

    LLM->>Backend: text: "Here's a summary of the report..."
    Backend->>Frontend: SSE: text (summary chunks)
    Backend->>Frontend: SSE: done

    Frontend->>Frontend: isFinalResult heuristic evaluates:<br/>✓ sessionArtifacts.length > 0<br/>✓ last agent message<br/>✓ tool activity before it<br/>✓ not streaming

    Frontend->>Frontend: Render ResultCard<br/>→ Summary text (ReactMarkdown)<br/>→ EmbeddedFileViewer (fetches full report)<br/>→ Download: Markdown / DOCX / PDF<br/>→ SourceCitations (if web search was used)
```

### The `isFinalResult` heuristic

This heuristic determines whether an `agent` message should render as a plain markdown bubble or as a `ResultCard` with embedded file viewers:

```typescript
const isFinalResult = !isStreaming &&
  !(msg as AgentMessage)._streaming &&
  (msg.content.length > 500 || sessionArtifacts.length > 0) &&
  !msg.content.includes('```astonish-app') &&
  !messages.slice(index + 1).some(m => m.type === 'agent') &&
  messages.slice(0, index).some(m =>
    m.type === 'tool_call' || m.type === 'tool_result' ||
    m.type === 'subtask_execution' || m.type === 'fleet_execution'
  )
```

Conditions:
1. **Not streaming** -- both the session and the message must be finalized
2. **Content length > 500 OR artifacts exist** -- short summaries trigger ResultCard when files were created
3. **No `astonish-app` fence** -- app fences go through the AppCodeIndicator path
4. **Last agent message** -- no later agent messages in the list
5. **Tool activity before it** -- must have tool_call, tool_result, subtask, or fleet activity preceding

### The `embeddedArtifactPaths` memo

When `isFinalResult` is true, the `embeddedArtifactPaths` set is populated with all session artifact paths. This suppresses the inline `ArtifactCard` for those files, since they're already shown inside the ResultCard's `EmbeddedFileViewer`. **This memo must mirror the `isFinalResult` logic exactly** -- if they diverge, artifacts either render twice or not at all.

### Export pipeline

| Format | Mechanism | Speed |
|--------|----------|-------|
| **Markdown** | `<a>.click()` → `GET /api/studio/artifacts` → serve file from disk | Instant |
| **DOCX** | Client-side: `@m2d/remark-docx` + `file-saver` `saveAs()` | ~1s |
| **PDF** | `fetch()` → `GET /api/studio/artifacts/pdf` → headless Chrome (go-rod) → blob → `saveAs()` | 5-15s |

PDF export uses `fetch()` + blob download (not `<a>.click()` navigation) because the backend PDF generation takes 5-15 seconds. The `<a>.click()` pattern causes the browser to show "Site wasn't available" for slow responses. The `fetch()` approach shows a loading spinner on the Download button during generation.

### Why reports use `write_file`, not inline fences

This was tested and the inline fence approach (`astonish-report` code fence, similar to `astonish-app`) was **rejected** because:

1. **No file on disk** -- the Files panel requires real files served by the artifact API. Without `write_file`, there's no file to serve, no artifact event, no Files button.
2. **No `EmbeddedFileViewer`** -- the `EmbeddedFileViewer` fetches file content from `/api/studio/artifacts`. Without a persisted file, it has nothing to fetch.
3. **No PDF/DOCX export** -- the PDF endpoint reads the markdown file from disk (or sandbox/JSONL). Without `write_file`, there's no file to convert.
4. **Dual rendering** -- the agent message still contains the raw fence text, rendering as a code-like collapsible block alongside the intended ResultCard. Stripping the fence from the agent message is fragile.

**Rule: Reports always use `write_file`. The LLM saves the full report to disk, then writes a concise summary in the chat.**

## The App Preview Pipeline

Visual apps use a different pipeline from reports. The LLM wraps generated React JSX in an `astonish-app` code fence, which the backend detects and emits as an `app_preview` event.

```mermaid
graph TD
    A["LLM generates text with<br/>```astonish-app fence"] --> B[Backend: detectAndEmitAppPreviews]
    B --> C["SSE: app_preview event<br/>{code, title, description, version}"]
    C --> D[Frontend: AppPreviewCard]
    D --> E[Sandboxed iframe<br/>Sucrase + React runtime]

    A --> F["During streaming:<br/>AppCodeIndicator shows progress"]
    F --> G["'Generating app...'<br/>pulsing animation"]
```

### Streaming progress indicator

During streaming, the agent message contains a partial `astonish-app` fence. The render loop detects this with a regex:

```typescript
const appFenceRe = /```astonish-app\s*\n([\s\S]*?)(?:\n```|$)/
```

When matched, it renders an `AppCodeIndicator` (pulsing "Generating app..." with expandable code view) instead of the raw fence text. After streaming completes, the `app_preview` event arrives and replaces the indicator with the full `AppPreviewCard`.

**This streaming interceptor is intentionally `astonish-app`-only.** Do not generalize it to other fence types without implementing the full backend event + frontend handler + ResultCard integration for that type.

## The Source Citations Pipeline

When the agent uses web search tools during a turn, source URLs are collected and displayed as clickable citation pills below the ResultCard.

```mermaid
graph LR
    A[tool_result events<br/>from web search tools] --> B[collectSourceUrls<br/>walks backward from agent msg]
    B --> C[extractUrlsFromResult<br/>deep-walks objects/arrays/strings]
    C --> D[SourceCitations component<br/>clickable URL pills]
```

This is a **frontend heuristic** (not a backend event). The function `collectSourceUrls(messages, agentIndex)` walks backward from the agent message, finds `tool_result` messages from web search tools (matched by `WEB_SEARCH_PATTERNS`), and extracts URLs using a regex. URLs are deduplicated and filtered (no localhost, no API endpoints).

`SourceCitations` renders below the `ResultCard` when `isFinalResult` is true, and below the regular agent bubble when the agent message has source URLs.

## Adding a New SSE Event Type

Follow these steps when adding a new event type to the pipeline:

### Step 1: Backend -- Emit the event

In `pkg/api/chat_runner.go` (or `chat_handlers.go` for slash command events):

```go
cr.emitEvent("my_new_event", map[string]any{
    "field1": value1,
    "field2": value2,
})
```

### Step 2: Frontend -- Define the message type

In `web/src/components/chat/chatTypes.ts`:

```typescript
export interface MyNewMessage {
  type: 'my_new_event'
  content: string
  field1: string
  field2: number
}
```

Add it to the `ChatMsg` union type.

### Step 3: Frontend -- Add SSE handlers (BOTH connect paths)

In `web/src/components/StudioChat.tsx`, add a `case` in **both** the `connectChat` handler and the `connectChatStream` handler:

```typescript
case 'my_new_event':
  setMessages((prev: ChatMsg[]) => [...prev, {
    type: 'my_new_event',
    content: data.content as string,
    field1: data.field1 as string,
    field2: data.field2 as number,
  } as MyNewMessage])
  break
```

### Step 4: Frontend -- Add rendering

In the render loop (`messages.map((msg, index) => { ... })`):

```typescript
if (msg.type === 'my_new_event') {
  const myMsg = msg as MyNewMessage
  return (
    <div key={index}>
      <MyNewComponent field1={myMsg.field1} field2={myMsg.field2} />
    </div>
  )
}
```

### Step 5: Write tests

1. **Fixture JSON** -- Create `web/src/test/fixtures/scenarios/<category>/my-new-event.json` with simulated SSE events
2. **Frontend scenario test** -- Create `web/src/test/scenarios/my-new-event.test.tsx` that loads the fixture, renders StudioChat, and asserts the component appears
3. **Backend integration test** -- Add a test in `pkg/api/integration_test.go` or `integration_gaps_test.go` using `MockLLM` to verify the event is emitted correctly
4. **Prompt contract** (if the event depends on system prompt instructions) -- Add an assertion in `pkg/agent/system_prompt_contracts_test.go`

See `docs/architecture/testing-chat-scenarios.md` for the full testing infrastructure.

## Sandbox Container Lifecycle

When the PDF export or browser tools need headless Chrome, they use the session's sandbox container. Containers can be in three states:

```mermaid
stateDiagram-v2
    [*] --> Running: EnsureSessionContainer<br/>(first use)
    Running --> Stopped: Idle watchdog<br/>or manual stop
    Stopped --> Running: EnsureSessionContainer<br/>(unmount stale overlay → remount → start)
    Running --> [*]: Session deleted<br/>(destroyOverlayContainer)
```

**Critical invariant:** When restarting a stopped container, the overlay filesystem mount must be unmounted and remounted before starting. After a container stops, the kernel's dentry cache for the overlay can become stale -- `ls rootfs/` returns empty even though `/proc/mounts` shows the overlay as mounted. Without a fresh remount, the container starts with an empty rootfs and crashes immediately.

This is handled in `EnsureSessionContainer` (`pkg/sandbox/lifecycle.go`):

```go
// Container stopped — unmount stale overlay before remounting
_ = umountOnSandboxHost(containerRootfs)
ensureOverlayMounted(client, containerName, ...)  // fresh mount
client.StartInstance(containerName)
```

## Rules and Invariants

These rules were established through bugs and fixes. Violating them will break the pipeline.

### Report rendering

1. **Reports must use `write_file`** -- The system prompt instructs the LLM to save reports to disk via `write_file`, then present a concise summary inline. Never use inline fences for reports.
2. **`isFinalResult` triggers `ResultCard`** when `sessionArtifacts.length > 0` OR `content.length > 500`, plus the last-agent and tool-activity-before conditions.
3. **`embeddedArtifactPaths` must mirror `isFinalResult`** -- They share the same detection logic to prevent duplicate artifact cards.

### Event handling

4. **Both connect paths must stay in sync** -- `connectChat` (new messages) and `connectChatStream` (reconnect) have parallel switch statements. Every new event type must be added to both.
5. **Finalize streaming text before special events** -- `app_preview` and similar events that arrive mid-stream must finalize the current streaming text first (commit the partial agent message) before adding their own message.

### Frontend heuristics vs backend events

6. **Prefer backend events for new features** -- Frontend heuristics (like `isFinalResult`, `collectSourceUrls`) exist for legacy reasons where the backend doesn't emit a dedicated event. For new features, emit a backend event rather than adding a frontend heuristic.
7. **The `astonish-app` streaming interceptor is app-only** -- Do not generalize the fence regex to match other `astonish-*` patterns without implementing the complete pipeline (backend detection → SSE event → frontend handler → component rendering).

### Export

8. **PDF export uses `fetch()` + `saveAs()`, not `<a>.click()`** -- The backend PDF generation takes 5-15 seconds. The `<a>.click()` navigation pattern causes "Site wasn't available" errors for slow responses. Always use `fetch()` → blob → `saveAs()` for slow download endpoints.

### Sandbox

9. **Unmount stale overlays before restarting stopped containers** -- `ensureOverlayMounted` must force a remount (unmount first) when restarting a stopped container. The `IsOverlayMounted` check alone is insufficient because the mount can have stale dentries.

## File Reference

### Backend

| File | Role |
|------|------|
| `pkg/api/chat_runner.go` | SSE event emission, tool execution, app preview detection |
| `pkg/api/chat_handlers.go` | HTTP handler, slash commands, redirect events |
| `pkg/api/session_handlers.go` | Artifact serving, PDF generation endpoint |
| `pkg/api/run_handler.go` | PDF browser manager, sandbox container resolution |
| `pkg/agent/guidance_content.go` | System prompt (report rules, app rules) |
| `pkg/pdfgen/chrome.go` | Headless Chrome PDF generation via go-rod |
| `pkg/sandbox/lifecycle.go` | Container lifecycle, overlay mount management |

### Frontend

| File | Role |
|------|------|
| `web/src/components/StudioChat.tsx` | Main component -- SSE handlers, state, render loop |
| `web/src/components/chat/chatTypes.ts` | Message type interfaces and ChatMsg union type |
| `web/src/api/studioChat.ts` | `connectChat()`, `connectChatStream()`, artifact APIs |
| `web/src/components/chat/ResultCard.tsx` | Report rendering with EmbeddedFileViewer, export |
| `web/src/components/chat/AppPreviewCard.tsx` | Sandboxed React preview in iframe |
| `web/src/components/chat/AppCodeIndicator.tsx` | Streaming progress for app generation |
| `web/src/components/chat/FilePanel.tsx` | Full-screen file viewer overlay |
| `web/src/components/chat/ArtifactCard.tsx` | Inline file notification card |
| `web/src/components/chat/TaskPlanPanel.tsx` | Delegation progress panel |
| `web/src/components/chat/FleetExecutionPanel.tsx` | Fleet multi-agent progress |
| `web/src/components/chat/PlanPanel.tsx` | Plan goal + step checklist |
| `web/src/components/chat/DistillPreviewCard.tsx` | Flow YAML preview |
| `web/src/components/chat/BrowserView.tsx` | VNC browser handoff |

### Test Infrastructure

| File | Role |
|------|------|
| `web/src/test/helpers/sseSimulator.ts` | Creates ReadableStream from fixture events |
| `web/src/test/helpers/mockFetch.ts` | Shared fetch interceptor with queue support |
| `web/src/test/helpers/renderChat.tsx` | StudioChat test wrapper |
| `web/src/test/scenarios/scenarioSetup.tsx` | Shared vi.mock() declarations |
| `web/src/test/fixtures/scenarios/` | JSON fixture files for SSE simulation |
| `pkg/api/integration_test.go` | Backend integration tests with MockLLM |
| `pkg/agent/system_prompt_contracts_test.go` | Prompt contract tests + golden file |
