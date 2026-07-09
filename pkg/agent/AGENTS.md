# pkg/agent — AGENTS.md

Core `ChatAgent` runtime — the tool-use loop that drives Astonish's autonomous chat. Everything that runs "one turn" of the agent goes through here.

## Scope
- `ChatAgent` construction and step execution.
- Prompt building (system prompt, tool descriptions, session context).
- Tool-call orchestration: model → tool call → tool result → model, with streaming.
- Sub-agent delegation (a `ChatAgent` may spawn up to 10 sub-agents with filtered tool access and isolated sessions).

## Interactions
- Wired by `pkg/launcher/chat_factory.go:NewWiredChatAgent` — this is where the full agent (LLM, tools, sandbox, memory, tool index, prompt builder) is assembled.
- Wrapped in an ADK agent by `pkg/launcher/chat_console.go:RunChatConsole` for the CLI, and by `pkg/api` chat handlers for Studio.
- Runs tools via `RunnableTool.Run` (see `pkg/tools/AGENTS.md`); tools that hit the shell/network/filesystem are wrapped by `pkg/sandbox` (see `pkg/sandbox/AGENTS.md`).

## Key rules
1. **The agent must not read config directly** — it receives its configuration from the factory. Keeps testing tractable.
2. **Streaming semantics**: partial model output is streamed as text events; tool calls are emitted as discrete events. Do not batch — Studio Chat relies on incremental delivery.
3. **Tool-call safety**: never execute a tool call without going through the `Backend`-wrapped path when the tool is sandbox-scoped.
4. **Sub-agent budget**: max 10 concurrent sub-agents by design (see the README and `docs/architecture/`). Do not raise this without discussion — it bounds fan-out cost.

## When editing
- Changing the tool-call loop? Update both the CLI console (`chat_console.go`) and the Studio SSE runner (`pkg/api/chat_runner.go`) — they consume the same agent but present different sinks.
- Changing prompt construction? Coordinate with the system-prompt contract tests (they enforce the two-step artifact/report protocol).
