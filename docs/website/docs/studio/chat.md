# Chat Interface

The Chat tab is Studio's primary interaction surface. It provides a rich conversation interface with streaming responses, tool call visualization, and artifact rendering.

## Streaming Responses

Chat uses Server-Sent Events (SSE) to stream agent responses in real time. Tokens appear as they're generated — no waiting for the full response before seeing output.

## Active Model Display

The top bar shows the currently active provider and model as a read-only indicator. To change which model is used, go to **Settings → Providers** and update the default. The change applies to all subsequent messages.

## Tool Call Visualization

When the agent invokes tools, the chat displays:

- **Tool name** and parameters (collapsible)
- **Execution status** (running, complete, error)
- **Output** formatted for readability (code blocks, tables)

Tool calls appear inline within the response stream, showing exactly when and how the agent uses its capabilities.

## Slash Commands

Type `/` in the input to access available commands:

| Command | Description |
|---------|-------------|
| `/help` | Show available commands |
| `/status` | Show provider, model, and tools info |
| `/new` | Start a fresh conversation |
| `/compact` | Show context window usage |
| `/distill` | Distill the current session into a reusable flow |
| `/fleet` | Start a fleet-based task with specialized agents |
| `/fleet-plan` | Create a reusable fleet plan |
| `/drill` | Create a drill suite with guided wizard |
| `/drill-add` | Add new drills to an existing suite |

## Artifact Rendering

When the agent produces artifacts (files, reports, applications), they render inline:

- **Markdown reports** — Rendered as formatted documents directly in the chat
- **Code files** — Syntax-highlighted with copy button
- **Applications** — Embedded preview with live interaction (see [Generative UI](../generative-ui/))

Non-report file writes appear as compact download tiles linking to the Files panel.

## Session Management

The sidebar lists previous sessions with timestamps and preview text. Click any session to resume it. Sessions can be:

- Resumed at any time
- Deleted when no longer needed

## Input Features

- **Multi-line input** — Shift+Enter for new lines
- **File attachment** — Drag and drop files into the input area
- **Slash command autocomplete** — Type `/` to see available commands with filtering
