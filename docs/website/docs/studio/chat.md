# Chat Interface

The Chat tab is Studio's primary interaction surface. It provides a rich conversation interface with streaming responses, tool call visualization, and artifact rendering.

## Streaming Responses

Chat uses Server-Sent Events (SSE) to stream agent responses in real time. Tokens appear as they're generated — no waiting for the full response before seeing output.

## Model Selector

The model dropdown in the chat header lets you switch providers and models mid-conversation:

- Select any configured provider (OpenAI, Anthropic, Google, etc.)
- Change models without starting a new session
- Token usage is tracked per-model

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
| `/new` | Start a new session |
| `/model <name>` | Switch model |
| `/flow <name>` | Execute a flow |
| `/clear` | Clear the display (session preserved) |

## Artifact Rendering

When the agent produces artifacts (files, reports, applications), they render inline:

- **Markdown reports** — Rendered as formatted documents directly in the chat
- **Code files** — Syntax-highlighted with copy button
- **Applications** — Embedded preview with live interaction (see [Generative UI](../generative-ui/))

Non-report file writes appear as compact download tiles linking to the Files panel.

## Session Management

The sidebar lists previous sessions with timestamps and preview text. Click any session to resume it. Sessions can be:

- Resumed at any time
- Forked to create a branch from a specific point
- Deleted when no longer needed

## Input Features

- **Multi-line input** — Shift+Enter for new lines
- **File attachment** — Drag and drop files into the input area
- **History navigation** — Arrow keys cycle through previous messages
