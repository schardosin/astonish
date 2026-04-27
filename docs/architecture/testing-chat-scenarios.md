# Testing Chat Scenarios

## Overview

The Studio Chat (`StudioChat.tsx`) is the primary user interface for Astonish. It handles 28 SSE event types, orchestrates 19 sub-components, manages 35 state variables, and interacts with 12+ API endpoints. Every new feature -- tool execution, task delegation, app generation, downloads -- flows through this single component.

Without structured scenario-based testing, regressions are invisible until they hit production. This document defines every testable scenario, the infrastructure required to test them, and the implementation plan.

### What We're Protecting Against

- **SSE event handling regressions**: A change to how `tool_call` events are processed could silently break tool rendering.
- **State management bugs**: The 35 useState variables in StudioChat interact in complex ways (streaming text finalization, panel mutual exclusion, session switching during active streams).
- **Component integration failures**: Sub-components like TaskPlanPanel, PlanPanel, and ResultCard depend on specific data shapes -- upstream changes can break downstream rendering.
- **Download/export pipeline breaks**: The DOCX and PDF conversion paths involve dynamic imports, backend endpoints, and format-specific logic that have no test coverage today.

### Current Test Coverage

| Area | Files | Lines | Coverage |
|------|-------|-------|----------|
| Frontend unit tests | 15 | ~1,931 | API functions, utils, shallow component renders |
| Go unit tests | 43+ | ~5,000+ | Handlers, agent logic, tools |
| **Chat integration tests** | **0** | **0** | **None** |
| **E2E tests** | **0** | **0** | **None** |

The gap is clear: we test individual functions but not the flows that users actually experience.

---

## Architecture

### Three-Layer Testing Strategy

```
Layer 3: E2E Smoke Tests (Playwright, future)
  - Real browser, real server, mock LLM
  - 5-10 critical paths only
  - Catches CSS/layout/browser-specific issues

Layer 2: Backend Integration Tests (Go)
  - Real HTTP server, mock LLM provider
  - Validates SSE event sequences end-to-end
  - Catches event ordering, serialization, state machine bugs

Layer 1: SSE Scenario Tests (Vitest + React Testing Library)     <-- PRIMARY
  - Simulated SSE streams from JSON fixtures
  - Real component tree, real state management, real SSE parsing
  - Catches UI regressions, state bugs, rendering issues
```

Layer 1 is the primary investment. It gives the highest coverage-to-effort ratio because it tests the real frontend code path (SSE parsing → state updates → component rendering) without requiring a running backend.

### SSE Scenario Test Infrastructure

```
web/src/test/
├── setup.ts                         # (existing) jest-dom + matchMedia mock
├── fixtures/
│   └── scenarios/
│       ├── core/
│       │   ├── simple-qa.json
│       │   ├── multi-chunk-streaming.json
│       │   ├── session-creation.json
│       │   ├── session-reconnection.json
│       │   ├── session-title-update.json
│       │   ├── new-session-command.json
│       │   ├── empty-response.json
│       │   └── stream-abort.json
│       ├── tools/
│       │   ├── single-tool-call.json
│       │   ├── parallel-tool-calls.json
│       │   ├── tool-with-image.json
│       │   ├── auto-approved-tool.json
│       │   ├── approval-flow.json
│       │   └── approval-multi-option.json
│       ├── delegation/
│       │   ├── simple-delegation.json
│       │   ├── multi-task-parallel.json
│       │   ├── task-with-tools.json
│       │   ├── task-failure.json
│       │   └── task-retry.json
│       ├── planning/
│       │   ├── plan-announced.json
│       │   ├── plan-step-transitions.json
│       │   ├── plan-auto-complete.json
│       │   ├── plan-partial-failure.json
│       │   └── plan-with-delegation.json
│       ├── apps/
│       │   ├── app-generated.json
│       │   ├── app-version-navigation.json
│       │   ├── app-save-flow.json
│       │   └── app-saved-confirmation.json
│       ├── errors/
│       │   ├── simple-error.json
│       │   ├── structured-error-info.json
│       │   ├── retry-event.json
│       │   └── connection-error.json
│       ├── distill/
│       │   ├── distill-preview.json
│       │   ├── distill-save.json
│       │   └── distill-cancel.json
│       ├── downloads/
│       │   ├── artifact-created.json
│       │   └── result-with-artifacts.json
│       ├── fleet/
│       │   ├── fleet-start.json
│       │   ├── fleet-message-exchange.json
│       │   ├── fleet-execution-progress.json
│       │   └── fleet-redirect.json
│       ├── browser/
│       │   └── browser-handoff.json
│       └── misc/
│           ├── thinking-message.json
│           ├── system-message.json
│           ├── usage-accumulation.json
│           └── mermaid-diagram.json
├── helpers/
│   ├── sseSimulator.ts              # Creates ReadableStream from fixture events
│   ├── mockFetch.ts                 # Shared fetch mock (replaces per-file duplication)
│   └── renderChat.ts               # StudioChat wrapper with mocked APIs + SSE
└── scenarios/
    ├── core-chat.test.tsx
    ├── tool-execution.test.tsx
    ├── tool-approval.test.tsx
    ├── task-delegation.test.tsx
    ├── plan-tracking.test.tsx
    ├── app-preview.test.tsx
    ├── error-handling.test.tsx
    ├── distill-flow.test.tsx
    ├── downloads-export.test.tsx
    ├── file-panel.test.tsx
    ├── panel-management.test.tsx
    ├── fleet-mode.test.tsx
    ├── browser-handoff.test.tsx
    ├── slash-commands.test.tsx
    ├── session-management.test.tsx
    ├── clipboard.test.tsx
    ├── usage-tracking.test.tsx
    └── wizard-flows.test.tsx
```

### JSON Fixture Format

Each fixture is a self-contained scenario with metadata and an ordered event sequence:

```json
{
  "name": "single-tool-call",
  "description": "Agent uses web_search tool then provides a text answer",
  "category": "tools",
  "events": [
    {
      "type": "session",
      "data": { "sessionId": "sess-001", "isNew": true },
      "delayMs": 0
    },
    {
      "type": "text",
      "data": { "text": "Let me search for that." },
      "delayMs": 50
    },
    {
      "type": "tool_call",
      "data": {
        "name": "web_search",
        "args": { "query": "Go testing best practices" }
      },
      "delayMs": 100
    },
    {
      "type": "tool_result",
      "data": {
        "name": "web_search",
        "result": "Found 5 results about Go testing..."
      },
      "delayMs": 200
    },
    {
      "type": "text",
      "data": { "text": "Based on my search, here are the best practices..." },
      "delayMs": 50
    },
    {
      "type": "usage",
      "data": { "input_tokens": 150, "output_tokens": 80, "total_tokens": 230 },
      "delayMs": 0
    },
    {
      "type": "done",
      "data": { "done": true },
      "delayMs": 0
    }
  ]
}
```

The `delayMs` field controls timing in the SSE simulator. For fast unit tests, delays can be set to 0. For integration/visual tests, realistic delays help verify streaming behavior.

### SSE Simulator (`sseSimulator.ts`)

Creates a `ReadableStream` that emits SSE-formatted text, matching the backend wire format:

```
event: <type>\ndata: <json>\n\n
```

The simulator:
- Encodes each fixture event as an SSE text block
- Supports configurable delays between events (or instant for unit tests)
- Can split events across chunk boundaries to test the parser's buffer reassembly
- Returns a `Response`-like object that `connectChat()` / `connectChatStream()` can consume via their `response.body.getReader()` path

### Shared Fetch Mock (`mockFetch.ts`)

Replaces the duplicated `mockFetch()` helpers currently copy-pasted across 4 API test files. Intercepts:

| URL Pattern | Response |
|-------------|----------|
| `POST /api/studio/chat` | SSE stream from scenario fixture |
| `GET /api/studio/sessions` | Mock session list |
| `GET /api/studio/sessions/:id` | Mock session history |
| `GET /api/studio/sessions/:id/status` | `{ running: false }` (configurable) |
| `DELETE /api/studio/sessions/:id` | `{ ok: true }` |
| `POST /api/studio/sessions/:id/stop` | `{ ok: true }` |
| `GET /api/studio/artifacts/content` | Mock file content |
| `GET /api/studio/fleet/sessions` | Empty list (configurable) |
| Other URLs | Passthrough to real fetch or error |

### Chat Renderer (`renderChat.ts`)

Wraps `render(<StudioChat />)` with:
- Pre-configured fetch mock loaded with a given scenario
- Required props (stubs for `onSessionChange`, etc.)
- Utility methods for common assertions:
  - `waitForEvent(type)` -- waits until an event of the given type has been processed
  - `getMessages()` -- returns the rendered message elements
  - `sendUserMessage(text)` -- simulates typing and submitting

---

## Scenario Catalog

### A. Core Chat Flow

#### A1. Simple Q&A
**Events:** `session` → `text` → `usage` → `done`
**Assertions:**
- Agent message appears with streamed text content
- `isStreaming` becomes true during streaming, false after `done`
- Usage data is captured (non-zero totalTokens)
- No error messages rendered

#### A2. Multi-Chunk Streaming
**Events:** `session` → `text("Hello ")` → `text("world, ")` → `text("how are you?")` → `done`
**Assertions:**
- Text accumulates correctly: final content is "Hello world, how are you?"
- Single agent message is created (not three separate ones)
- `_streaming` flag is true during streaming, removed after `done`

#### A3. Session Creation
**Events:** `session(sessionId: "new-123", isNew: true)` → `text` → `done`
**Assertions:**
- `activeSessionId` set to "new-123"
- Sessions list refreshes (loadSessions called after 500ms delay)

#### A4. Session Reconnection
**Precondition:** Session "existing-456" has an active runner (`fetchSessionStatus` returns `running: true`)
**Events (replayed from history + live):** `session` → `text("partial...")` → `tool_call` → `tool_result` → `text("final answer")` → `done`
**Assertions:**
- Messages are cleared before replay (no stale data)
- All replayed events render correctly
- `isStreaming` is true during replay, false after `done`

#### A5. Session Title Update
**Events:** `session` → `text` → `session_title(title: "Go Testing Tips")` → `done`
**Assertions:**
- Session in sidebar list updates to show "Go Testing Tips"
- No message is added to the chat for the title event

#### A6. New Session via `/new`
**Events:** `new_session(sessionId: "fresh-789")` → `done`
**Assertions:**
- `activeSessionId` changes to "fresh-789"
- Messages array is cleared
- Sessions list refreshes

#### A7. Empty Response Safety
**Events:** `session` → `error("The model returned an empty response...")` → `done`
**Assertions:**
- Error message rendered in red error box
- No agent message is created
- Streaming state resets

#### A8. Stream Abort (Stop Button)
**Events (partial):** `session` → `text("Starting to..."`
**User action:** Click Stop button
**Assertions:**
- SSE connection aborted (`abortRef.current.abort()` called)
- `isStreaming` becomes false
- `stopChat(sessionId)` API called
- Partial text is preserved as a finalized message (or cleaned up if empty)

---

### B. Tool Execution

#### B1. Single Tool Call
**Events:** `session` → `text("Let me search.")` → `tool_call(name: "web_search", args: {...})` → `tool_result(name: "web_search", result: "...")` → `text("Here's what I found.")` → `usage` → `done`
**Assertions:**
- Initial text finalized before tool_call (streaming text committed)
- ToolCallMessage rendered with tool name and args
- ToolResultMessage rendered with tool name and result
- Final text rendered as separate agent message
- Tool card shows tool name

#### B2. Parallel Tool Calls
**Events:** `session` → `text` → `tool_call("web_search")` → `tool_call("read_file")` → `tool_result("web_search")` → `tool_result("read_file")` → `text` → `done`
**Assertions:**
- Both tool calls rendered in order
- Both tool results rendered in order
- Each tool card shows its respective name

#### B3. Tool Card Expand/Collapse
**Precondition:** Scenario with tool_call + tool_result already rendered
**User action:** Click on tool card header
**Assertions:**
- Tool args/result toggle visibility
- `expandedTools` set updated correctly
- Clicking again collapses

#### B4. Streaming Text Finalization Before Tool Call
**Events:** `session` → `text("Analyzing...")` → `text(" your request.")` → `tool_call("search")`
**Assertions:**
- Accumulated text "Analyzing... your request." is committed as a finalized (non-streaming) agent message
- `streamingTextRef` is cleared
- `tool_call` appears after the finalized text

#### B5. Image Tool Result
**Events:** `session` → `tool_call("screenshot")` → `tool_result("screenshot")` → `image(data: "base64...", mimeType: "image/png")` → `done`
**Assertions:**
- ImageMessage rendered with `<img>` tag
- `src` attribute is `data:image/png;base64,...`
- Both `data` and `mimeType` are truthy (guard condition)

#### B6. Auto-Approved Tool
**Events:** `session` → `auto_approved(tool: "shell_command")` → `tool_call("shell_command")` → `tool_result("shell_command")` → `text` → `done`
**Assertions:**
- Green checkmark badge rendered with tool name "shell_command"
- Tool execution proceeds normally after auto-approval

#### B7. Artifact from Tool
**Events:** `session` → `tool_call("write_file")` → `tool_result("write_file")` → `artifact(path: "/workspace/report.md", tool_name: "write_file")` → `text` → `done`
**Assertions:**
- `sessionArtifacts` array updated with new entry
- ArtifactCard rendered inline with filename "report.md" and FilePlus icon
- Files button appears in toolbar with badge count "1"

---

### C. Tool Approval Flow

#### C1. Approval Prompt with Options
**Events:** `session` → `text` → `approval(tool: "shell_command", options: ["Allow", "Deny"])` (stream pauses, waiting for user)
**Assertions:**
- ApprovalMessage rendered with tool name "shell_command"
- Two buttons rendered: "Allow" and "Deny"
- Clicking "Allow" calls `sendMessage("Allow")`

#### C2. Approval with Empty Options
**Events:** `session` → `approval(tool: "dangerous_tool", options: [])`
**Assertions:**
- Approval prompt renders with tool name
- No option buttons rendered (empty options array)
- Prompt text still visible

#### C3. Multi-Option Approval
**Events:** `session` → `approval(tool: "web_fetch", options: ["Allow once", "Allow always", "Deny"])`
**Assertions:**
- Three buttons rendered with correct labels
- Each button click sends the corresponding option string as a message

---

### D. Task Delegation (delegate_tasks)

#### D1. Simple Delegation
**Events:**
```
session →
subtask_progress(event_type: "delegation_start", tasks: [{name: "Research", description: "Find info"}]) →
subtask_progress(event_type: "task_start", task_name: "Research") →
subtask_progress(event_type: "task_complete", task_name: "Research", status: "success", duration: "3.2s") →
subtask_progress(event_type: "delegation_complete", status: "success") →
text("Here's what I found.") → done
```
**Assertions:**
- SubTaskExecutionMessage created with `status: "running"`
- TaskPlanPanel renders with "Research" task
- Task transitions: pending → running (spinner) → complete (green check)
- Duration "3.2s" displayed
- Final text rendered after delegation

#### D2. Multi-Task Parallel Delegation
**Events:**
```
session →
subtask_progress(delegation_start, tasks: [
  {name: "Research", description: "Find competitors"},
  {name: "Analysis", description: "Analyze market"},
  {name: "Report", description: "Write summary"}
]) →
subtask_progress(task_start, task_name: "Research") →
subtask_progress(task_start, task_name: "Analysis") →
subtask_progress(task_tool_call, task_name: "Research", tool_name: "web_search", tool_args: {...}) →
subtask_progress(task_tool_result, task_name: "Research", tool_name: "web_search", tool_result: "...") →
subtask_progress(task_complete, task_name: "Research", status: "success", duration: "5.1s") →
subtask_progress(task_start, task_name: "Report") →
subtask_progress(task_complete, task_name: "Analysis", status: "success", duration: "4.3s") →
subtask_progress(task_complete, task_name: "Report", status: "success", duration: "2.8s") →
subtask_progress(delegation_complete, status: "success") →
text → done
```
**Assertions:**
- Three tasks shown in TaskPlanPanel
- Tasks run concurrently (Research and Analysis both "running" simultaneously)
- Each task shows its own duration on completion
- Expanding "Research" shows its tool_call and tool_result events

#### D3. Task with Tool Calls
**Events:**
```
session →
subtask_progress(delegation_start, tasks: [{name: "Search"}]) →
subtask_progress(task_start, task_name: "Search") →
subtask_progress(task_tool_call, task_name: "Search", tool_name: "web_search", tool_args: {query: "..."}) →
subtask_progress(task_tool_result, task_name: "Search", tool_name: "web_search", tool_result: "...") →
subtask_progress(task_text, task_name: "Search", text: "Found relevant results.") →
subtask_progress(task_complete, task_name: "Search", status: "success") →
subtask_progress(delegation_complete) → done
```
**Assertions:**
- Expanding "Search" task shows tool_call card, tool_result card, and text block
- Tool card displays "web_search" with args
- Text block renders markdown

#### D4. Task Failure
**Events:**
```
session →
subtask_progress(delegation_start, tasks: [{name: "Deploy"}]) →
subtask_progress(task_start, task_name: "Deploy") →
subtask_progress(task_failed, task_name: "Deploy", status: "error", error: "Connection refused", duration: "1.5s") →
subtask_progress(delegation_complete, status: "error") →
text("The deployment failed.") → done
```
**Assertions:**
- Task shows red status indicator with "!" icon
- Error text "Connection refused" visible (truncated if long)
- Duration shown despite failure
- Delegation status is "error"

#### D5. Task Retry
**Events:**
```
session →
subtask_progress(delegation_start, tasks: [{name: "API Call"}]) →
subtask_progress(task_start, task_name: "API Call") →
subtask_progress(task_failed, task_name: "API Call", status: "error", error: "429 rate limited") →
subtask_progress(task_retry, task_name: "API Call", error: "429 rate limited") →
subtask_progress(task_start, task_name: "API Call") →
subtask_progress(task_complete, task_name: "API Call", status: "success", duration: "4.0s") →
subtask_progress(delegation_complete, status: "success") → done
```
**Assertions:**
- Task transitions: pending → running → failed → running (retrying) → complete
- "Retrying" indicator shown with `RotateCcw` icon during retry phase
- Error and duration cleared on retry, new duration shown on completion

#### D6. TaskPlanPanel Expand/Collapse
**Precondition:** Delegation with completed tasks rendered
**User action:** Click task row
**Assertions:**
- Task events (tool calls, text) toggle visibility
- Chevron rotates (down when expanded, right when collapsed)
- "Waiting to start..." shown for pending tasks with no events

#### D7. TaskPlanPanel Content Truncation
**Precondition:** Task with tool_result containing >800 characters
**Assertions:**
- Content truncated at 800 chars with "Show more" link
- Clicking "Show more" reveals full content, changes to "Show less"
- Clicking "Show less" truncates again

---

### E. Plan Announcement & Tracking

#### E1. Plan Announced
**Events:**
```
session →
subtask_progress(event_type: "plan_announced", plan_goal: "Research competitors", plan_steps: [
  {name: "Find competitors", description: "Search for main competitors"},
  {name: "Analyze pricing", description: "Compare pricing models"},
  {name: "Write report", description: "Summarize findings"}
]) →
...
```
**Assertions:**
- PlanPanel renders with goal "Research competitors"
- Three steps shown, all with "pending" status (gray circles)
- Status badge shows "Submitted"
- Todo button in toolbar shows badge "0/3"

#### E2. Plan Step Running
**Events (continuing from E1):**
```
subtask_progress(event_type: "plan_step_update", step_name: "Find competitors", step_status: "running")
```
**Assertions:**
- "Find competitors" step shows spinning Loader icon
- Step text changes to primary color
- Status badge changes to "In Progress"

#### E3. Plan Step Complete
**Events (continuing):**
```
subtask_progress(event_type: "plan_step_update", step_name: "Find competitors", step_status: "complete")
```
**Assertions:**
- "Find competitors" step shows green Check icon
- Step text gets muted color + line-through decoration
- Todo button badge updates to "1/3"

#### E4. Plan Step Failed
**Events:**
```
subtask_progress(event_type: "plan_step_update", step_name: "Analyze pricing", step_status: "failed")
```
**Assertions:**
- "Analyze pricing" step shows red AlertCircle icon
- Step text in failed styling

#### E5. Plan Auto-Complete
**Events:** Full plan with 3 steps, only step 1 explicitly completed, then `done`
**Assertions:**
- After `done`, all remaining pending/running steps marked "complete" by `CompleteAll()`
- Status badge shows "Complete"
- Overall panel opacity reduces to 0.6

#### E6. Plan Partial Completion
**Events:** Plan with 3 steps; step 1 complete, step 2 failed, step 3 complete
**Assertions:**
- Status badge shows "Partial" (not "Complete") due to failure
- Badge color is warning (not green)

#### E7. TodoPanel
**Precondition:** Plan exists in messages
**User action:** Click Todo button in toolbar
**Assertions:**
- TodoPanel opens on right side
- Shows plan goal and steps with correct status icons
- Close button works
- Files panel and Apps panel close (mutual exclusion)

**Empty state:** No plan messages → "No plan yet -- the agent will share its plan here"

---

### F. Plan + Delegation Combined

#### F1. Full Plan-to-Delegation Workflow
**Events:**
```
session →
subtask_progress(plan_announced, goal: "Analyze market", steps: ["Research", "Compare", "Report"]) →
subtask_progress(plan_step_update, step_name: "Research", step_status: "running") →
subtask_progress(delegation_start, tasks: [{name: "Research competitors"}, {name: "Research trends"}]) →
subtask_progress(task_start, task_name: "Research competitors") →
subtask_progress(task_start, task_name: "Research trends") →
subtask_progress(task_complete, task_name: "Research competitors") →
subtask_progress(task_complete, task_name: "Research trends") →
subtask_progress(delegation_complete, status: "success") →
subtask_progress(plan_step_update, step_name: "Research", step_status: "complete") →
subtask_progress(plan_step_update, step_name: "Compare", step_status: "running") →
subtask_progress(delegation_start, tasks: [{name: "Price comparison"}]) →
... (more delegation) ...
subtask_progress(plan_step_update, step_name: "Compare", step_status: "complete") →
text("Here is the full analysis.") → usage → done
```
**Assertions:**
- PlanPanel and TaskPlanPanel both render (PlanPanel for high-level steps, TaskPlanPanel for delegation details)
- Plan step status transitions correctly tied to delegation completion
- Final "Report" step auto-completes at `done`
- Both panels show correct final states

#### F2. Step Status Tied to Task Completion
**Assertions:**
- Plan step "Research" stays "running" until ALL tasks registered under it complete
- Only when both "Research competitors" and "Research trends" complete does "Research" transition to "complete"

#### F3. Multiple Delegations Across Steps
**Assertions:**
- Each delegation creates a separate SubTaskExecutionMessage
- Each plan step update corresponds to the correct delegation
- TaskPlanPanel shows latest delegation (searching from end of messages)

---

### G. App Preview / Generative UI

#### G1. App Generated
**Events:** `session` → `text("Here's the app:")` → `app_preview(code: "...", title: "Dashboard", version: 1, appId: "app-001")` → `done`
**Assertions:**
- Streaming text finalized before app_preview
- AppPreviewCard renders with title "Dashboard"
- `activeAppId` set to "app-001"
- Apps button appears in toolbar

#### G2. App Version Navigation
**Events:**
```
session →
app_preview(code: "v1...", title: "Dashboard", version: 1, appId: "app-001") →
text("Let me improve it.") →
app_preview(code: "v2...", title: "Dashboard", version: 2, appId: "app-001") →
done
```
**Assertions:**
- Only the latest version (v2) renders (earlier returns null)
- Version counter shows "2 of 2"
- Prev button navigates to v1, Next button disabled at v2

#### G3. App Save Flow
**Precondition:** Active app preview rendered
**User actions:** Click Save → Enter name "My Dashboard" → Click Confirm
**Assertions:**
- Save dialog opens with text input
- `sendMessage("__app_save__:My Dashboard")` called (not shown as user message)
- On `app_saved` event: green "App Saved" card rendered
- `activeAppId` cleared to null
- `astonish:apps-updated` window event dispatched

#### G4. App Code Viewer Toggle
**Precondition:** AppPreviewCard rendered
**User action:** Click code toggle button
**Assertions:**
- Source code panel appears below the preview
- Code displayed in `<pre><code>` block
- Copy button copies code to clipboard
- Toggle again hides the panel

#### G5. App Fullscreen
**User action:** Click fullscreen button
**Assertions:**
- Fixed overlay covers viewport
- `document.body.style.overflow` set to "hidden"
- Escape key or close button exits fullscreen
- Body overflow restored on exit

#### G6. AppCodeIndicator (Streaming vs Done)
**During streaming:** Shows "Generating app..." with spinner
**After streaming:** Shows "Generated app" with line count badge
**User action:** Click to expand/collapse code view

#### G7. AppsPanel
**Precondition:** Multiple apps generated in chat
**User action:** Click Apps button in toolbar
**Assertions:**
- Panel opens listing all apps grouped by appId
- Active app shows "Refining" badge with Zap icon
- Multi-version apps show version count badge
- Clicking app opens fullscreen overlay with version navigation
- Code toggle and copy work in overlay

---

### H. App Preview Iframe Communication

#### H1. Sandbox Ready → Code Render → Success
**Assertions:**
- On `sandbox_ready` postMessage: `ready` state becomes true
- Code sent to iframe via `postMessage({ type: 'render', code })`
- On `render_success`: iframe height updates (capped at maxHeight)
- Error state cleared

#### H2. Render Error
**Assertions:**
- On `render_error` postMessage: error state set
- Red error box rendered below iframe
- Error message displayed

#### H3. Data Request Relay
**Assertions:**
- Iframe sends `data_request(sourceId, args, requestId)`
- Parent calls `fetchAppData(sourceId, args, requestId, appName)`
- Response relayed back as `data_response(requestId, data)`
- Errors relayed as `data_response(requestId, error)`

#### H4. Action Request Relay
**Same pattern as H3** with `action_request` / `fetchAppAction` / `action_response`

#### H5. AI Request Relay
**Same pattern as H3** with `ai_request` / `fetchAppAI` / `ai_response`

#### H6. State Operations with Validation
**Assertions:**
- `state_query` / `state_exec` require non-empty `appName`
- If `appName` is empty: error response "App name is required for state operations"
- If valid: proxied to `fetchAppStateQuery` / `fetchAppStateExec`

---

### I. Error Handling

#### I1. Simple Error
**Events:** `session` → `error(error: "Rate limit exceeded")` → `done`
**Assertions:**
- Red error box rendered with text "Rate limit exceeded"
- Fallback chain: `data.error` → `data.message` → "Unknown error"

#### I2. Structured Error Info
**Events:**
```
session → error_info(
  title: "Provider Error",
  reason: "API key is invalid",
  suggestion: "Check your API key in Settings > Providers",
  originalError: "HTTP 401: Unauthorized"
) → done
```
**Assertions:**
- ErrorInfoMessage rendered with title, reason, and suggestion sections
- Each section has distinct styling
- Original error available in collapsible `<details>`

#### I3. Error Info Raw Details
**Precondition:** ErrorInfoMessage with `originalError`
**User action:** Click to expand raw details
**Assertions:**
- `<details>` element opens showing original error text
- Pre-formatted text preserves error formatting

#### I4. Retry Event
**Events:** `session` → `text` → `retry(attempt: 1, maxRetries: 3, reason: "LLM stream was truncated")` → `text` → `done`
**Assertions:**
- Orange retry indicator rendered
- Shows "Attempt 1 of 3" and reason text
- Subsequent text continues normally after retry

#### I5. SSE Connection Error
**Trigger:** `onError` callback fires (network error, HTTP error)
**Assertions:**
- Error message appended to messages
- `isStreaming` set to false
- No dangling streaming state

---

### J. Distill Flow

#### J1. Distill Preview
**Events:**
```
session →
text("Analyzing your conversation...") →
distill_preview(
  yaml: "name: research_flow\nnodes:...",
  flowName: "research_flow",
  description: "Automated research pipeline",
  tags: ["research", "web"],
  explanation: "## Summary\n..."
) → done
```
**Assertions:**
- Streaming text finalized before distill_preview
- DistillPreviewCard renders with flow name, description, and tags
- Explanation section parsed into structured sections (summary, nodes, params, notes)
- Action buttons visible: Save, Request Changes, Cancel

#### J2. Distill Save
**User action:** Click "Save Flow"
**Events after action:** `distill_saved(filePath: "flows/research.yaml", runCommand: "astonish run research_flow")`
**Assertions:**
- `sendMessage("__distill_save__")` called (not displayed as user message)
- Green "Flow Saved" card rendered with file path and run command
- Copy button copies run command to clipboard
- `astonish:flows-updated` window event dispatched

#### J3. Distill Cancel
**User action:** Click "Cancel"
**Events after action:** `text("Distill review cancelled.")`
**Assertions:**
- `sendMessage("__distill_cancel__")` called
- System text "Distill review cancelled." appears

#### J4. Distill Request Changes
**User action:** Click "Request Changes"
**Assertions:**
- Input textarea gains focus
- Placeholder text changes (guidance for what to modify)
- No message sent automatically -- user types their modification request

#### J5. Distill Explanation Parsing
**Input markdown:**
```markdown
## Summary
This flow automates research.

## Nodes
- **search_web** (agent): Searches the internet
- **analyze** (agent): Analyzes results

## Parameters
- **topic**: The research topic
- **depth**: How deep to search

## Notes
Run with caution on rate-limited APIs.
```
**Assertions:**
- Summary section rendered
- 2 nodes rendered in grid with type badges and colors
- 2 parameters rendered
- Notes section rendered
- Unknown node types get muted default colors

---

### K. Download & Export

#### K1. ResultCard Download as Markdown
**Precondition:** Long agent response rendered as ResultCard
**User action:** Click download button in ResultCard header
**Assertions:**
- `Blob` created with content type `text/markdown`
- `URL.createObjectURL` called
- Anchor element click triggered with `download: "response.md"`
- `URL.revokeObjectURL` called (cleanup)

#### K2. EmbeddedFileViewer Download as Markdown
**Precondition:** Artifact with rendered EmbeddedFileViewer in ResultCard
**User action:** Open download dropdown → Click "Download as Markdown"
**Assertions:**
- Anchor element created with `href` pointing to `getArtifactDownloadUrl(path, sessionId)`
- `download` attribute set to original filename

#### K3. EmbeddedFileViewer Download as DOCX
**Precondition:** Markdown artifact in EmbeddedFileViewer
**User action:** Open download dropdown → Click "Download as DOCX"
**Assertions:**
- `exporting` state set to `'docx'` (spinner shown)
- Dynamic imports: `@m2d/remark-docx` and `file-saver`
- Pipeline: `unified().use(remarkParse).use(remarkGfm).use(remarkDocx).process(content)`
- `saveAs(blob, "filename.docx")` called
- `exporting` reset to null on completion

**DOCX unavailable for non-markdown:** Download option not shown when `fileType !== 'Markdown'`

#### K4. EmbeddedFileViewer Download as PDF
**Precondition:** Markdown artifact in EmbeddedFileViewer
**User action:** Open download dropdown → Click "Download as PDF"
**Assertions:**
- Anchor element created with `href` pointing to `getArtifactPDFUrl(path, sessionId)`
- `download` attribute set to "filename.pdf"
- Backend handles: markdown → goldmark HTML → Chrome headless → PDF

**PDF unavailable for non-markdown:** Download option not shown when `fileType !== 'Markdown'`

#### K5-K7. FilePanel Downloads
**Same three patterns as K2-K4**, but triggered from the FilePanel overlay instead of EmbeddedFileViewer. Identical download logic, same API endpoints.

#### K8. ArtifactCard Direct Download
**Precondition:** ArtifactCard rendered inline in chat
**User action:** Click download button on ArtifactCard
**Assertions:**
- `window.open(getArtifactDownloadUrl(path, sessionId), '_blank')` called
- No format selection -- always downloads raw file

---

### L. Artifact & File Management

#### L1. Artifact Event Processing
**Events:** `session` → `artifact(path: "/workspace/output.md", tool_name: "write_file")` → `done`
**Assertions:**
- `sessionArtifacts` updated with `{ path, fileName: "output.md", fileType: "Markdown", toolName: "write_file" }`
- ArtifactCard rendered with FilePlus icon (write_file) or Edit3 icon (edit_file)
- Files toolbar button appears with badge "1"

#### L2. Artifact Deduplication
**Events:** Two `artifact` events with same path
**Assertions:**
- `sessionArtifacts` contains only one entry for that path
- Two ArtifactCards rendered in chat (events are distinct messages)

#### L3. Artifact Open in FilePanel
**User action:** Click "Open in Files panel" on ArtifactCard
**Assertions:**
- `filePanelOpen` set to true
- `filePanelInitialPath` set to artifact path
- FilePanel overlay auto-opens to that file

#### L4. Embedded Artifact Suppression
**Precondition:** Session with artifacts and a long final agent message (>500 chars) after tool activity
**Assertions:**
- `embeddedArtifactPaths` memo computes the set of paths to suppress
- Matching ArtifactCards return null (not rendered inline)
- Artifacts instead appear inside the final ResultCard's EmbeddedFileViewer

#### L5. FilePanel File Selection
**Precondition:** FilePanel open with multiple artifacts
**User action:** Click a file in the sidebar list
**Assertions:**
- Overlay opens with file content
- `fetchArtifactContent(path, sessionId)` called
- Markdown files rendered via ReactMarkdown
- Non-markdown files rendered as `<pre>`
- Loading spinner during fetch
- Error message on fetch failure

#### L6. FilePanel Empty State
**Precondition:** No artifacts in session
**Assertions:**
- "No files generated yet" message
- Files button hidden from toolbar (shown only when `sessionArtifacts.length > 0`)

---

### M. ResultCard

#### M1. Long Response Renders as ResultCard
**Precondition:** Agent message with content >500 chars, preceded by tool activity, and is the last agent message
**Assertions:**
- Rendered as `<ResultCard>` instead of plain markdown bubble
- Markdown rendered with custom components (mermaid support)
- Header shows copy, raw toggle, fullscreen, download buttons

#### M2. Raw/Formatted Toggle
**User action:** Click code/eye button
**Assertions:**
- Switches between `<ReactMarkdown>` rendering and `<pre>` raw text
- Toggle icon changes accordingly
- Content preserved between switches

#### M3. ResultCard Fullscreen
**User action:** Click fullscreen button
**Assertions:**
- Fixed overlay renders with full content
- All toolbar actions (copy, raw, download) available in fullscreen
- Close button or Escape exits
- Body scroll locked during fullscreen

#### M4. ResultCard Collapse/Expand
**User action:** Click footer chevron
**Assertions:**
- Content area hides (collapsed)
- Chevron rotates (ChevronUp ↔ ChevronDown)
- Click again restores content

#### M5. ResultCard Copy
**User action:** Click copy button
**Assertions:**
- `navigator.clipboard.writeText(content)` called
- Check icon replaces copy icon for 2 seconds
- Returns to copy icon after timeout

---

### N. Panel Management

#### N1. Todo Panel Toggle
**User action:** Click Todo button in toolbar
**Assertions:**
- `todoPanelOpen` becomes true
- `filePanelOpen` and `appsPanelOpen` become false (mutual exclusion)
- Badge shows "done/total" step counts from latest plan

#### N2. Files Panel Toggle
**User action:** Click Files button in toolbar
**Assertions:**
- `filePanelOpen` becomes true
- `todoPanelOpen` and `appsPanelOpen` become false
- Badge shows artifact count

#### N3. Apps Panel Toggle
**User action:** Click Apps button in toolbar
**Assertions:**
- `appsPanelOpen` becomes true
- `todoPanelOpen` and `filePanelOpen` become false
- Badge shows unique app count
- Button only visible when messages contain `app_preview` type

#### N4. Mutual Exclusion
**User actions:** Open Todo → Open Files → Open Apps → Open Todo
**Assertions:**
- At each step, only one panel is open
- Previous panel closes before new one opens

#### N5. Usage Popover
**Precondition:** Streaming active with usage events
**User action:** Click Usage button
**Assertions:**
- Popover opens showing input/output/total token counts
- Elapsed time ticks during streaming (1-second interval)
- Token counts formatted (k for thousands, M for millions)
- Outside click closes popover
- When not streaming: "Send a message to see token usage" if no data

---

### O. Fleet Mode

#### O1. Fleet Start
**User action:** Click Fleet button → Select plan → Enter message → Click Start
**Assertions:**
- FleetStartDialog opens
- `fetchFleetPlans()` called, plans loaded
- Only `channel_type: "chat"` plans shown
- `startFleetSession(null, message, planKey)` called on submit
- `isFleetMode` set to true
- Fleet header bar appears with fleet name

#### O2. Fleet Message Send
**User action:** Type message and press Enter in fleet mode
**Assertions:**
- `sendFleetMessage(fleetSessionId, input)` called
- Optimistic user message rendered immediately (right-aligned "You" bubble)

#### O3. Fleet Message Deduplication
**Events:** `fleet_message(id: "msg-1", sender: "customer", text: "Hello")`
**Assertions:**
- If optimistic message exists without ID, it's replaced with server version
- If message with `id: "msg-1"` already exists, skip (no duplicate)

#### O4. Fleet Agent Message
**Events:** `fleet_message(sender: "architect", text: "I'll design the system.")`
**Assertions:**
- Colored agent card rendered with `@architect` badge
- Color from `getAgentColor("architect")`

#### O5. Fleet Progress Events
**Events:** `fleet_progress` events with phases
**Assertions:**
- FleetExecutionPanel renders with phase timeline
- Phases expand/collapse on click
- Status transitions (running → complete → failed)

#### O6. Fleet Execution Phases
**Assertions:**
- Running phases show spinner, auto-expand
- Completed phases show check, collapse
- Failed phases show red indicator
- Orchestrator events render inline (no dedicated phase row)

#### O7. Fleet Done
**Events:** `fleet_done`
**Assertions:**
- `isStreaming` set to false
- UI returns to idle state

#### O8. Fleet Exit
**User action:** Click "Exit Fleet" button
**Assertions:**
- `stopFleetSession(fleetSessionId)` called
- Fleet state cleared (isFleetMode, fleetSessionId, fleetInfo, fleetState)
- Sessions list refreshed
- Chat returns to normal mode

#### O9. Fleet Redirect
**Events (from `/fleet` command):** `fleet_redirect(task: "Build a web scraper")`
**Assertions:**
- `isStreaming` set to false
- FleetStartDialog opens with pre-filled message "Build a web scraper"

#### O10. Fleet Template Picker
**Events (from `/fleet-plan` without key):** `fleet_plan_redirect` with no hint
**Assertions:**
- FleetTemplatePicker dialog opens
- `fetchFleets()` called
- User selects template → `/fleet-plan <key>` sent

---

### P. Browser Handoff

#### P1. Browser Handoff Triggered
**Events:**
```
session → tool_call("browser_request_human") →
tool_result(name: "browser_request_human", result: {
  vnc_proxy_url: "http://...:6901/vnc.html",
  page_url: "https://example.com/login",
  page_title: "Login Page",
  reason: "CAPTCHA detected"
}) → ...
```
**Assertions:**
- BrowserHandoffMessage created (not a regular ToolResultMessage)
- BrowserView renders with VNC iframe
- Page URL and title displayed
- Reason text shown

#### P2. Browser Handoff Done
**User action:** Click "Done" button in BrowserView
**Assertions:**
- `fetch('/api/browser/handoff-done', { method: 'POST' })` called (best-effort)
- `isDone` state set to true
- View switches to green "Browser sharing ended" card

#### P3. BrowserView Fullscreen
**User action:** Click fullscreen button
**Assertions:**
- Fullscreen overlay with full-size VNC iframe
- Close button appears
- Escape key exits

---

### Q. Thinking & System Messages

#### Q1. Thinking Message
**Events:** `session` → `thinking(text: "Let me analyze this step by step...")` → `text` → `done`
**Assertions:**
- ThinkingMessage rendered with `.thinking-note` styling
- Content displayed correctly

#### Q2. System Message
**Events:** `system(content: "**Available commands:**\n- /help\n- /status")`
**Assertions:**
- SystemMessage rendered with indigo background and Info icon
- Markdown content rendered

#### Q3. Empty Thinking Filtered
**Events:** `thinking(text: "")` or `thinking(text: null)`
**Assertions:**
- No ThinkingMessage added to messages (guard: only if `data.text` is truthy)

---

### R. Slash Commands

#### R1. Slash Popup Activation
**User action:** Type "/" in input
**Assertions:**
- Slash popup appears with filtered command list
- All 9 commands visible when filter is empty
- Typing more characters filters the list

#### R2. Slash Popup Navigation
**User actions:** ArrowDown, ArrowDown, Tab
**Assertions:**
- `slashIndex` increments on ArrowDown
- Tab selects the highlighted command
- Escape closes popup

#### R3. `/help` Response
**Events after `/help`:** `system(content: "**Available commands:**...")` → `done`
**Assertions:**
- System message rendered with command list
- No user message shown for `/help` (slash commands start with `/`)

#### R4. `/status` Response
**Events:** `system(content: "**Status**\n- Provider: openai\n- Model: gpt-4...")` → `done`
**Assertions:**
- System message with provider/model info

#### R5. `/compact` Response
**Events:** `system(content: "**Context Window**\n...")` → `done`
**Assertions:**
- System message with context window usage info

#### R6. Unknown Command
**Events after `/foo`:** `system(content: "Unknown command: \`/foo\`...")` → `done`
**Assertions:**
- System message with error about unknown command

---

### S. Session Management

#### S1. Session List Loading
**On mount:**
**Assertions:**
- `isLoadingSessions` is true, spinner shown
- `fetchSessions()` called
- Sessions populated in sidebar after response
- `isLoadingSessions` becomes false

#### S2. Session Search
**User action:** Type "research" in session search input
**Assertions:**
- `filteredSessions` only includes sessions with "research" in title/id
- Filtered list updates live as user types

#### S3. Session Delete
**User action:** Click trash icon on a session
**Assertions:**
- `e.stopPropagation()` prevents session selection
- `deleteSession(sessionId)` called
- Session removed from local state
- If deleted session was active: messages cleared, fleet mode exited

#### S4. Sidebar Collapse/Expand
**User action:** Click collapse button
**Assertions:**
- `sidebarCollapsed` becomes true
- Sidebar shrinks to icon-only mode
- Click expand button restores full sidebar

#### S5. New Session
**User action:** Click "+" button
**Assertions:**
- Active stream aborted
- Fleet mode exited if active
- `activeSessionId` set to null
- Messages, artifacts, panels, token usage all cleared
- Input textarea focused

---

### T. Clipboard Operations

#### T1. Copy Agent Message
**User action:** Click copy button next to agent message
**Assertions:**
- `navigator.clipboard.writeText(msg.content)` called
- `copiedIndex` set to message index
- Check icon shown for 2 seconds, then reverts

#### T2. Copy App Code
**User action:** Click copy in AppPreviewCard code panel
**Assertions:**
- `navigator.clipboard.writeText(displayedData.code)` called

#### T3. Copy Distill Run Command
**User action:** Click copy button on DistillSavedMessage
**Assertions:**
- `navigator.clipboard.writeText(savedMsg.runCommand)` called

#### T4. Copy in AppsPanel Overlay
**User action:** Click copy in Apps overlay code view
**Assertions:**
- Version code copied, "Copied" feedback shown for 2 seconds

---

### U. Mermaid Diagrams

#### U1. Mermaid Code Block Rendering
**Precondition:** Agent message contains ` ```mermaid\ngraph TD\nA-->B\n``` `
**Assertions:**
- `markdownComponents` detects `language-mermaid` class
- `MermaidBlock` lazy-loads mermaid library
- SVG rendered via `dangerouslySetInnerHTML`
- `securityLevel: 'strict'` in mermaid config

#### U2. Mermaid Render Error
**Precondition:** Invalid mermaid syntax
**Assertions:**
- Error state set
- Red-bordered box with "Diagram render error: ..." message
- Raw mermaid source shown in `<pre>` fallback

#### U3. Mermaid Loading State
**Assertions:**
- While mermaid library loads: "Rendering diagram..." placeholder
- Placeholder has subtle bordered styling

---

### V. Usage Tracking

#### V1. Usage Accumulation
**Events:** `usage(input: 100, output: 50, total: 150)` → `usage(input: 200, output: 100, total: 300)`
**Assertions:**
- Token values **accumulate** (add, not replace)
- Final: `{ inputTokens: 300, outputTokens: 150, totalTokens: 450 }`

#### V2. Token Format Display
**Assertions:**
- `formatTokenCount(500)` → "500"
- `formatTokenCount(1500)` → "1.5k"
- `formatTokenCount(2500000)` → "2.5M"

#### V3. Elapsed Timer
**During streaming:**
- Timer starts from `sessionStartTime`
- Updates every 1 second
- `formatDuration(5000)` → "5s"
- `formatDuration(125000)` → "2m 5s"

**After streaming stops:**
- Timer stops (interval cleared)
- Last elapsed value preserved

---

### W. Wizard Flows

#### W1. Fleet Plan Wizard
**Events (from `/fleet-plan hint`):** `fleet_plan_redirect(hint: "monitoring", wizard_description: "...", wizard_system_prompt: "You are a fleet planning assistant...")`
**Assertions:**
- `activeWizardContext` set to the system prompt
- `pendingFleetPlanPrompt` set with message + systemContext
- Deferred prompt fires: `sendMessage(message, { systemContext })` on next render
- Subsequent user messages include `systemContext` in POST body

#### W2. Drill Wizard
**Events (from `/drill`):** `drill_redirect(hint: "API testing", wizard_system_prompt: "You are a drill creation assistant...")`
**Assertions:**
- `activeWizardContext` set
- `pendingDrillPrompt` set
- Multi-turn conversation with persistent system context

#### W3. Drill-Add Wizard
**Events (from `/drill-add my_suite`):** `drill_add_redirect(suite_name: "my_suite", wizard_system_prompt: "...")`
**Assertions:**
- Same wizard flow as W2 but with suite-specific kickoff message
- System context maintained across turns

#### W4. Wizard Context Cleared on Save
**Events during wizard:** Tool result for `save_fleet_plan` or `save_drill`
**Assertions:**
- `activeWizardContext` set to null
- Subsequent messages no longer include system context

---

### X. Backend Integration Scenarios (Phase 2 -- Go Tests)

These scenarios validate the backend event production pipeline. They require a running HTTP server with a mock LLM provider.

#### X1. ChatRunner Simple Chat
**Setup:** Mock LLM returns a text response
**Assertions:**
- Event sequence: `session` → `text` (one or more) → `usage` → `done`
- Session ID is valid
- Usage metadata present

#### X2. ChatRunner Tool Execution
**Setup:** Mock LLM returns a FunctionCall, then a text response
**Assertions:**
- Event sequence: `session` → `tool_call(name, args)` → `tool_result(name, result)` → `text` → `usage` → `done`
- `announce_plan` FunctionCalls are suppressed (not emitted as tool_call)

#### X3. ChatRunner Retry on Stream Truncation
**Setup:** Mock LLM returns truncated response, then succeeds on retry
**Assertions:**
- Event sequence includes: `retry(attempt: 1, maxRetries: 1, reason: "LLM stream was truncated...")` → `text` → `usage` → `done`

#### X4. SubTaskProgressCallback Delegation Sequence
**Setup:** Mock LLM calls `delegate_tasks`, sub-agents use tools
**Assertions:**
- Event sequence: `subtask_progress(delegation_start)` → `subtask_progress(task_start)` → `subtask_progress(task_tool_call)` → `subtask_progress(task_tool_result)` → `subtask_progress(task_complete)` → `subtask_progress(delegation_complete)`

#### X5. Plan State Step Progression
**Setup:** Mock LLM calls `announce_plan` then `delegate_tasks` with `plan_step` bindings
**Assertions:**
- Steps transition: pending → running → complete
- `ResolveStepName` matches tasks to steps (exact match or prefix)
- `CompleteAll` fires at end of turn

#### X6. History Reconstruction
**Setup:** Persisted session with tool calls and delegation
**Assertions:**
- `eventsToMessages` produces correct message types
- Tool calls grouped by invocation ID
- Delegation events reconstructed into `subtask_execution` messages

#### X7. Slash Command Responses
**Setup:** Send `/help`, `/status`, `/compact`, `/new`, `/distill`, unknown command
**Assertions:**
- Each produces correct SSE event type and data
- `/new` creates a new session
- Unknown command returns error in `system` event

#### X8. Artifact Download 3-Tier Fallback
**Setup:** Artifact path that exists only in session JSONL (not on host filesystem or sandbox)
**Assertions:**
- `StudioArtifactDownloadHandler` falls through host → sandbox → JSONL
- Correct content returned with appropriate Content-Type

#### X9. PDF Generation Pipeline
**Setup:** Markdown artifact content
**Assertions:**
- `StudioArtifactPDFHandler` reads markdown, converts via goldmark + Chrome
- Returns `application/pdf` with `Content-Disposition: attachment`
- 30-second timeout enforced

---

## Infrastructure Design

### SSE Simulator (`web/src/test/helpers/sseSimulator.ts`)

```typescript
interface FixtureEvent {
  type: string;
  data: Record<string, unknown>;
  delayMs?: number;
}

interface ScenarioFixture {
  name: string;
  description: string;
  category: string;
  events: FixtureEvent[];
}

/**
 * Creates a ReadableStream that emits SSE-formatted text chunks.
 * Matches the backend wire format: "event: <type>\ndata: <json>\n\n"
 */
function createSSEStream(
  events: FixtureEvent[],
  options?: { instant?: boolean; chunkSplit?: boolean }
): ReadableStream<Uint8Array>;

/**
 * Creates a Response-like object suitable for mocking fetch() calls
 * to /api/studio/chat.
 */
function createSSEResponse(
  events: FixtureEvent[],
  options?: { status?: number; instant?: boolean }
): Response;

/**
 * Loads a fixture JSON file by path and returns the parsed scenario.
 */
function loadFixture(path: string): ScenarioFixture;
```

### Shared Fetch Mock (`web/src/test/helpers/mockFetch.ts`)

```typescript
interface MockFetchConfig {
  scenarioEvents?: FixtureEvent[];        // Events for /api/studio/chat
  sessions?: ChatSession[];                // Response for /api/studio/sessions
  sessionHistory?: SessionHistory;         // Response for /api/studio/sessions/:id
  sessionStatus?: { running: boolean };    // Response for /api/studio/sessions/:id/status
  artifactContent?: Record<string, string>; // path -> content map
  fleetSessions?: FleetSession[];
  fleetPlans?: FleetPlanSummary[];
}

/**
 * Sets up globalThis.fetch with intelligent routing.
 * Returns a cleanup function.
 */
function setupMockFetch(config: MockFetchConfig): () => void;

/**
 * Shared mock response builder (replaces per-file duplication).
 */
function mockResponse(data: unknown, ok?: boolean, statusText?: string): Response;
```

### Chat Renderer (`web/src/test/helpers/renderChat.ts`)

```typescript
interface RenderChatOptions {
  scenarioEvents?: FixtureEvent[];
  initialSessionId?: string;
  sessions?: ChatSession[];
  mockConfig?: Partial<MockFetchConfig>;
}

interface RenderChatResult {
  // Standard RTL result
  ...RenderResult;
  // Custom helpers
  waitForStreamComplete(): Promise<void>;
  getMessageElements(): HTMLElement[];
  sendMessage(text: string): Promise<void>;
  clickButton(label: string): Promise<void>;
}

/**
 * Renders StudioChat with mocked APIs and optional SSE scenario.
 */
function renderChat(options?: RenderChatOptions): RenderChatResult;
```

### Mock LLM Server (for Go Integration Tests)

```go
// pkg/api/testutil/mock_llm.go

// MockLLMServer is a test HTTP server that mimics an LLM API.
// It returns deterministic responses based on preconfigured scenarios.
type MockLLMServer struct {
    server    *httptest.Server
    scenarios map[string]MockResponse
}

type MockResponse struct {
    Text          string
    FunctionCalls []FunctionCall
    ThinkingText  string
    Usage         UsageMetadata
}

// NewMockLLMServer creates a server that responds to /v1/chat/completions.
func NewMockLLMServer() *MockLLMServer

// AddScenario registers a response for messages matching a pattern.
func (m *MockLLMServer) AddScenario(pattern string, response MockResponse)

// URL returns the server's base URL for use as a provider endpoint.
func (m *MockLLMServer) URL() string
```

This mock LLM server is reusable across Go integration tests (Phase 2) and could later serve as the backend for Playwright E2E tests (Phase 3).

---

## Implementation Status

### Completed

#### Layer 1 — SSE Scenario Tests (Vitest)
- **24 test files, 87 scenario tests, 35 JSON fixture files** — all passing
- Infrastructure: `sseSimulator.ts`, `mockFetch.ts` (with queue support and `onChatRequest` callback), `renderChat.tsx` (with `data-testid` support), `scenarioSetup.tsx` (shared mocks)
- Categories covered: core chat (A1-A2, A5-A8), tools (B1-B2, B6-B7), delegation (D1-D2, D4-D5), planning (E1-E3, F1), apps (G1-G3), errors (I1-I4), distill (J1-J2), downloads (K8, L1), fleet (O5, O9), browser (P1), sessions (S1-S5), slash commands (R1-R3), panels (N1-N4), clipboard (T1), misc (Q1-Q3, V1, U1)
- **Phase 2 additions:** multi-turn conversations, tool Deny button, reconnection, session switch while streaming

#### Layer 2 — Backend Integration Tests (Go)
- **5 files, 54 integration tests** — all passing, 0 data races
- `MockLLM` with turn-based queue, `TruncationMockLLM`, `BlockingLLM`
- Scenarios X1-X10 + 17 expanded tests (state deltas, truncation retry, multi-turn, context cancellation, subscriber management, app preview refinement)
- **Phase 2 additions (P0):** Tool error propagation (3 tests), parallel tool dispatch (2 tests), SSE HTTP handler wire format (3 tests), approval flow with autoApprove=false (3 tests)

#### Layer 3 — Prompt Contract Tests (Go)
- **1 file, 31 tests** — golden file snapshot + 60+ structural assertions
- Multi-configuration tests (9) for conditional sections
- Size regression guards (minimal ≤5,100B, maximal ≤10,700B)

#### Pre-existing test fixes
- Fixed `act()` warnings in App, SettingsPage, SetupWizard, StudioChat tests
- Suppressed expected console noise
- Fixed button-inside-button HTML violation in StudioChat.tsx sidebar

#### Infrastructure improvements (Phase 2)
- Extracted 6 duplicated `vi.mock()` blocks from 20 test files into shared `scenarioSetup.tsx`
- Added queue-based `mockFetch` for multi-turn conversations (`FixtureEvent[] | FixtureEvent[][]`)
- Added `onChatRequest` callback for request body validation
- Added `data-testid` attributes to `StudioChat.tsx` (`chat-input`, `message-area`, `send-button`)
- Updated `renderChat.tsx` to prefer `data-testid` selectors with fallback to regex patterns

### Current Counts

| Layer | Files | Tests |
|-------|-------|-------|
| L1: SSE Scenario Tests | 24 | 87 |
| L2: Backend Integration | 5 | 54 |
| L3: Prompt Contracts | 1 | 31 |
| Pre-existing unit tests (Go) | 102 | ~1,345 |
| Pre-existing unit tests (Frontend) | 13 | ~98 |
| **Total** | **145** | **~1,615** |

---

## Gap Analysis & Remediation

An external review of the test suite identified coverage gaps across three priority tiers.
Each gap was verified against the codebase.

### P0 — High-Risk Gaps (Backend)

#### Gap 1: Tool execution errors untested
**Status:** Verified TRUE — `newMockTool()` at `integration_test.go:278` hardcodes `nil` error return. No mock tool ever returns an error. Error propagation from tool → runner → event stream is dark.

**Fix:** Add `newMockErrorTool(name, errMsg)` variant. Add tests:
- Tool returns error → verify `error` event with tool name and message
- Tool panics → verify error event (not crash)

#### Gap 2: Parallel tool dispatch untested
**Status:** Verified TRUE — `MultiToolCallTurn` defined at `mock_llm_test.go:154` but never called in any test. Zero tests exercise LLM returning multiple function calls simultaneously.

**Fix:** Add test using `MultiToolCallTurn` — verify all tool_call events emitted, all tool_result events emitted, ordering consistent.

#### Gap 3: SSE HTTP handler wire format untested
**Status:** Verified TRUE — Integration tests call `runner.Run()` directly, bypassing HTTP entirely. No test validates SSE framing (`event: <type>\ndata: <json>\n\n`), Content-Type headers, or flushing.

**Fix:** Add `httptest.NewRecorder()` test exercising the actual `streamRunnerEvents()` → `SendSSE()` path. Verify Content-Type, event framing, valid JSON, `done` is last.

#### Gap 4: Full approval flow (autoApprove=false) untested
**Status:** Partially verified — State delta tests (X11, X11b, X12, X12b) DO test approval event emission via `processStateDelta()`. But `runAndCollect()` hardcodes `autoApprove: true` at `integration_test.go:94`. The full pause→resume state machine (ProtectedTool returns `ErrWaitingForApproval` → runner terminates → next message resumes) is never exercised end-to-end.

**Fix:** Add integration test with `autoApprove=false`. Verify: first run emits approval event and terminates, second run (simulating "Yes") executes tool and completes.

### P1 — Infrastructure Hardening

#### Gap 5: Duplicated vi.mock() blocks
**Status:** Verified TRUE — All 20 scenario test files repeat identical `vi.mock()` blocks (react-markdown, remark-gfm, HomePage, FleetStartDialog, FleetTemplatePicker, MermaidBlock).

**Fix:** Extract to `web/src/test/scenarios/setup.ts`. Register as `setupFiles` in vitest config (scoped to scenario tests) or use shared import.

#### Gap 6: mockFetch returns same events for every POST
**Status:** Verified TRUE — `config.scenarioEvents` is set once and returned for every `POST /api/studio/chat`. No mechanism for call-count-aware or body-aware responses.

**Fix:** Accept `scenarioEvents` as `FixtureEvent[] | FixtureEvent[][]`. When an array-of-arrays, shift the first item on each POST call. Enables multi-turn conversation testing.

#### Gap 7: No frontend multi-turn conversation test
**Status:** Verified TRUE — Each `it()` block sends exactly one message. No test exercises send→receive→send→receive.

**Fix:** Add test using queue-based mockFetch (Gap 6). Send 2 messages, verify both responses render.

#### Gap 8: Tool Deny button never clicked
**Status:** Verified TRUE — `tool-execution.test.tsx:173` asserts the Deny button exists (`expect(denyBtn).toBeDefined()`) but never clicks it.

**Fix:** Add test in `tool-interactions.test.tsx` that clicks Deny, verifies the rejection message is sent.

#### Gap 9: reconnectEvents infrastructure unused
**Status:** Verified TRUE — `renderChat.tsx:24` accepts `reconnectEvents` and `mockFetch.ts:100` routes it, but zero scenario tests use it.

**Fix:** Add reconnection scenario test using the existing infrastructure.

### P2 — Nice to Have

#### Gap 10: Fragile placeholder regex selectors
**Status:** Verified TRUE — `renderChat.tsx` uses `getByPlaceholderText(/type.*message|ask.*anything/i)` and `querySelector('[class*="messages"]')`. No `data-testid` attributes exist anywhere in StudioChat.tsx or its 19 sub-components.

**Fix:** Add `data-testid="chat-input"`, `data-testid="message-area"`, `data-testid="send-button"` to StudioChat.tsx. Update `renderChat.tsx` helpers to use `getByTestId()`.

#### Gap 11: mockFetch never validates request bodies
**Status:** Verified TRUE — Chat POST handler at `mockFetch.ts:91-96` checks method and URL only. `init?.body` is never read.

**Fix:** Add optional `onChatRequest` callback to mockFetch config. Parse request body, pass to callback for assertion. Add one test verifying correct `{ message, sessionId }` shape.

#### Gap 12: No session-switch-while-streaming test
**Status:** Not yet verified but plausible — no test starts a stream and then changes sessions mid-stream.

**Fix:** Add scenario test that starts SSE events flowing, then simulates clicking a different session. Verify old stream stops, no crash, no orphaned state.

---

## Implementation Priorities

### Phase 1: Infrastructure + Core Scenarios ✅ COMPLETE

**Delivered:** 79 frontend scenario tests + 43 backend integration tests + 31 prompt contract tests.

### Phase 2: Gap Remediation ✅ COMPLETE

**Delivered:** 11 new backend tests (P0 gaps) + 8 new frontend scenario tests + infrastructure improvements.

| Item | Priority | Layer | What | Status |
|------|----------|-------|------|--------|
| 1 | P0 | L2 (Go) | Tool execution error tests | ✅ 3 tests |
| 2 | P0 | L2 (Go) | Parallel tool dispatch tests | ✅ 2 tests |
| 3 | P0 | L2 (Go) | SSE HTTP handler wire format test | ✅ 3 tests |
| 4 | P0 | L2 (Go) | Full approval flow (autoApprove=false) | ✅ 3 tests |
| 5 | P1 | L1 (Frontend) | Extract duplicated vi.mock() to shared setup | ✅ scenarioSetup.tsx |
| 6 | P1 | L1 (Frontend) | Multi-turn mockFetch support (event queue) | ✅ queue mode |
| 7 | P1 | L1 (Frontend) | Frontend multi-turn conversation test | ✅ 2 tests |
| 8 | P1 | L1 (Frontend) | Tool Deny button click test | ✅ 2 tests |
| 9 | P1 | L1 (Frontend) | Reconnection test | ✅ 2 tests |
| 10 | P2 | L1 (Frontend) | Add data-testid attributes, replace regex selectors | ✅ 3 attributes |
| 11 | P2 | L1 (Frontend) | Request body validation in mockFetch | ✅ onChatRequest callback |
| 12 | P2 | L1 (Frontend) | Session switch while streaming test | ✅ 2 tests |

### Phase 3: Remaining Scenarios (Future)

Scenarios from the catalog not yet implemented. See sections B3-B5, C1-C3, D3/D6-D7, E4-E7, F2-F3, G4-G7, H1-H6, J3-J5, K1-K7, L2-L6, M1-M5, N5, O1-O8/O10, R4-R6, T2-T4, U2-U3, V2-V3, W1-W4.

### Phase 4: E2E Smoke Tests (Future)

- Playwright setup with mock LLM backend
- 5-10 critical paths: send message, tool execution, delegation, download, session switch
- Run in CI on merges to `main` only (not every PR -- too slow)
- Visual regression snapshots for key states

---

## Fixture Authoring Guidelines

### Event Data Shapes Reference

Every fixture event must match the backend's wire format. Here are the canonical data shapes for each event type:

| Event Type | Required Data Fields |
|------------|---------------------|
| `session` | `{ sessionId: string, isNew: boolean }` |
| `text` | `{ text: string }` |
| `tool_call` | `{ name: string, args: Record<string, any> }` |
| `tool_result` | `{ name: string, result: any }` |
| `image` | `{ data: string (base64), mimeType: string }` |
| `artifact` | `{ path: string, tool_name: string }` |
| `flow_output` | `{ content: string }` |
| `approval` | `{ tool: string, options: any[] }` |
| `auto_approved` | `{ tool: string }` |
| `thinking` | `{ text: string }` |
| `retry` | `{ attempt: number, maxRetries: number, reason: string }` |
| `error` | `{ error: string }` or `{ message: string }` |
| `error_info` | `{ title: any, reason: any, suggestion: any, originalError: any }` |
| `usage` | `{ input_tokens: number, output_tokens: number, total_tokens: number }` |
| `session_title` | `{ title: string }` |
| `system` | `{ content: string }` |
| `new_session` | `{ sessionId: string }` |
| `done` | `{ done: true }` |
| `app_preview` | `{ code: string, title: string, description: string, version: number, appId?: string }` |
| `app_saved` | `{ name: string, path: string }` |
| `distill_preview` | `{ yaml: string, flowName: string, description: string, tags: string[], explanation: string }` |
| `distill_saved` | `{ filePath: string, runCommand: string }` |
| `fleet_redirect` | `{ task?: string }` |
| `fleet_plan_redirect` | `{ hint?: string, wizard_description?: string, wizard_system_prompt?: string }` |
| `drill_redirect` | `{ hint?: string, wizard_system_prompt?: string }` |
| `drill_add_redirect` | `{ suite_name: string, wizard_system_prompt: string }` |
| `fleet_progress` | `{ type: string, phase?: string, agent?: string, ... }` (FleetEvent shape) |
| `fleet_message` | `{ id?: string, sender: string, text: string, timestamp?: string }` |
| `fleet_session` | `{ fleet_key: string, fleet_name: string, agents: string[] }` |
| `fleet_state` | `{ state: string, active_agent?: string }` |
| `fleet_done` | `{}` |

### `subtask_progress` Sub-Event Data Shapes

The `subtask_progress` event carries an `event_type` field that determines the rest of the data:

| `event_type` | Additional Fields |
|-------------|-------------------|
| `plan_announced` | `plan_goal: string, plan_steps: [{name, description}]` |
| `plan_step_update` | `step_name: string, step_status: "pending" \| "running" \| "complete" \| "failed"` |
| `delegation_start` | `tasks: [{name, description, plan_step?}]` |
| `delegation_complete` | `status: "success" \| "error" \| "partial"` |
| `task_start` | `task_name: string, plan_step?: string` |
| `task_complete` | `task_name: string, status: "success", duration: string` |
| `task_failed` | `task_name: string, status: "error" \| "timeout", duration: string, error: string` |
| `task_retry` | `task_name: string, error: string` |
| `task_tool_call` | `task_name: string, tool_name: string, tool_args: any` |
| `task_tool_result` | `task_name: string, tool_name: string, tool_result: any` |
| `task_text` | `task_name: string, text: string` |

### Naming Conventions

- Fixture files: `kebab-case.json` matching the scenario name
- Test files: `kebab-case.test.tsx` matching the feature area
- Scenario names in fixtures: lowercase with hyphens (e.g., `"simple-delegation"`)
- Categories: match the directory name (e.g., `"delegation"`, `"tools"`, `"planning"`)

### Fixture Validation

Each fixture should be self-contained and always end with a `done` event (or `fleet_done` for fleet scenarios). The test infrastructure should validate this on load and warn if a fixture is missing a terminal event.
