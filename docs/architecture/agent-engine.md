# Agent Execution Engine

## Overview

The agent execution engine is the core of Astonish -- it processes user messages, orchestrates LLM calls, manages tool execution, and produces responses. There are two distinct agent types serving different use cases:

- **ChatAgent**: Open-ended conversational agent. No predefined flow. The LLM decides which tools to call and how to proceed. This is the primary mode used in Studio, console, and external channels.
- **AstonishAgent**: Flow-based agent that follows YAML-defined node graphs with LLM nodes, tool nodes, and conditional branching. Used for deterministic, repeatable workflows.

Both agents are built on top of **Google's Agent Development Kit (ADK)**, specifically wrapping ADK's `llmagent` for LLM interaction and `runner` for session-aware execution.

## Key Design Decisions

### Why Wrap ADK Instead of Using It Directly

ADK provides a solid foundation (session management, tool dispatch, streaming) but Astonish needs capabilities that ADK doesn't offer:

- **Dynamic tool injection**: Tools are discovered per-turn via semantic search, not statically registered.
- **Credential security**: Before/after tool callbacks must substitute and restore credential placeholders without leaking secrets into session history.
- **Context compaction**: When the context window fills up, the system must compress history without losing critical information.
- **Auto-knowledge retrieval**: Every turn triggers a vector search to inject relevant guidance into the system prompt.
- **Execution tracing**: Every tool call is recorded for flow distillation and memory reflection.
- **Think-tag filtering**: Chain-of-thought content (`<think>` blocks) must be stripped from streaming output.

ADK's callback system (BeforeToolCallbacks, AfterToolCallbacks, BeforeModelCallbacks) provides the extension points, but the coordination between them is Astonish-specific.

### Why a Three-Tier System Prompt

The system prompt uses a deliberate tiered architecture to balance token efficiency with comprehensive guidance:

- **Tier 1 (Static Core)**: Identity, behavior rules, tool usage guidelines, environment info, capability listing. ~800 tokens. Stable across turns so LLM providers can cache the KV prefix.
- **Tier 2 (Indexed Guidance)**: Detailed how-to documentation for each capability (browser, credentials, scheduling, etc.). Stored as `memory/guidance/*.md` files indexed in the vector store. Zero tokens in the prompt until retrieved. This keeps the base prompt small for models with limited context windows.
- **Tier 3 (Per-Turn Dynamic)**: Auto-retrieved knowledge, relevant tool descriptions, channel hints, scheduler context, session-specific instructions. Appended at the end of the system prompt so the static prefix remains cacheable.

### Why Sequential Tool Dispatch

ADK processes tool calls sequentially within a single invocation (a for-loop, not goroutines). This simplifies credential substitution: a shared `var credentialRestore func()` variable between the before and after callbacks is safe without synchronization. If ADK ever moves to concurrent tool dispatch, the credential flow would need per-call scoping.

### Why Hybrid Tool Discovery

Most agent frameworks require all tools to be declared upfront. With 60+ built-in tools plus MCP tools, this wastes context window tokens listing tools the user doesn't need. Instead, Astonish uses a two-layer approach:

- **Static tools**: A small core set (file ops, shell, memory) always available.
- **Dynamic injection**: Before each LLM call, a `BeforeModelCallback` adds tool declarations based on semantic search matches and explicit `search_tools` calls. The LLM sees only relevant tools.

## Architecture

### ChatAgent Execution Flow

```
User Message
    |
    v
1. Secret Extraction: PendingVault.Extract() replaces <<<secret>>> with <<<SECRET_N>>> tokens
    |
    v
2. Knowledge Retrieval: Two partitioned vector searches
   - Guidance docs (max 3, min score 0.3) -- how-to instructions
   - General knowledge (max 5, min score 0.3) -- memory, skills, flows
   Results injected into SystemPromptBuilder.RelevantKnowledge
    |
    v
3. Tool Discovery: Hybrid search on ToolIndex
   - Vector similarity + BM25 keyword matching (RRF fusion)
   - Top 8 matches formatted for prompt + stored for dynamic injection
    |
    v
4. System Prompt Build: SystemPromptBuilder.Build()
   - Static core (~800 tokens) + per-turn dynamic content
    |
    v
5. LLM Agent Creation: llmagent.New() with callbacks
   - BeforeToolCallbacks: credential substitution, secret token resolution
   - AfterToolCallbacks: credential restoration, redaction, trace recording, image stripping
   - BeforeModelCallbacks: tool response truncation, dynamic tool injection, context compaction
    |
    v
6. Execution Loop (with retry):
   - llmAgent.Run() produces streaming events
   - Retryable errors (429, 502, 503) -> exponential backoff, retry up to 3x
   - Unknown tool errors (hallucinated name) -> synthetic error response, re-run
   - Tool call count cap (default 25) -> pause and ask user to continue
   - Approval pause -> yield event and return, resume on next user message
    |
    v
7. Post-Task Processing:
   - Memory reflection: silent LLM call to identify durable knowledge worth saving
   - Trace storage: execution trace saved for on-demand /distill
```

### AstonishAgent Execution Flow

The AstonishAgent follows a YAML-defined node graph:

```
YAML Flow Definition
    |
    v
Parse nodes: START -> node_1 -> node_2 -> ... -> END
    |
    v
For each node:
  - LLM node: executeLLMNode() with intelligent retry
  - Tool node: executeToolNode() with direct tool invocation
  - Condition: evaluateCondition() to choose next node
    |
    v
State machine: each node reads/writes session state
    |
    v
Approval gates: if tool is protected, pause for user approval
```

LLM nodes within flows use the same callback architecture as ChatAgent (credential substitution, redaction, tracing) but with flow-specific error recovery: an `ErrorRecoveryNode` uses a separate LLM call to analyze failures and decide whether to retry with a modified strategy or abort.

### Tool Callback Architecture

The callback chain runs for every tool call:

```
LLM requests tool call
    |
    v
BeforeToolCallback #1: Credential Substitution
  - Scans args for {{CREDENTIAL:name:field}} placeholders
  - Replaces with real values in-place (same map ADK uses)
  - Stores restore function in shared variable
    |
    v
BeforeToolCallback #2: Secret Token Resolution
  - Scans args for <<<SECRET_N>>> tokens
  - Replaces with real values from PendingVault
  - Stores restore function
    |
    v
Tool Executes (with real credential values)
    |
    v
AfterToolCallback:
  1. Restore credential placeholders (undoes in-place substitution)
  2. Restore secret tokens (undoes in-place substitution)
  3. Redact any credential values from tool output
  4. Strip image_base64 from output (stash for channel delivery)
  5. Strip large flow output (stash for direct delivery)
  6. Record step in execution trace
  7. After save_credential: retroactively redact session transcript
```

The critical invariant: the session event (which shares the same args map by reference due to an ADK design choice) always retains placeholder tokens, never real secrets.

### Dynamic Tool Injection

The `DynamicToolInjectionCallback` is a `BeforeModelCallback` that fires on every LLM API call (including after tool results):

1. Collects tool matches from the per-turn hybrid search (set during knowledge retrieval).
2. Collects tool names from any `search_tools` calls made within the current turn.
3. For each match, resolves the concrete `tool.Tool` implementation from the `ToolIndex` registry.
4. Adds these tools to the `LLMRequest.Tools` array so the LLM can call them.

This means the LLM's available toolset can grow mid-turn as `search_tools` discovers additional tools.

### Sub-Agent System

The `SubAgentManager` enables the ChatAgent to delegate work to specialized child agents via the `delegate_tasks` tool:

- **Concurrent execution**: Multiple sub-agents run in parallel, each with its own session, tool set, and optional model override.
- **Tool groups**: Tools are organized into named groups (core, browser, mcp:*). The LLM specifies which groups each sub-agent needs.
- **Depth limiting**: Default max depth of 2 prevents infinite delegation chains.
- **Container sharing**: Sub-agent sessions are aliased to the parent's sandbox container via `NodeClientPool.Alias()`.
- **Event forwarding**: When `UIEventCallback` is set, sub-agent events (tool calls, text) are streamed to the UI in real-time.

Each sub-agent gets its own system prompt built by `buildChildPrompt()` which includes the task instructions, available tool names, and a reminder that it's a focused worker with a specific mission.

### Think-Tag Filtering

Some models (especially open-source ones) emit chain-of-thought in `<think>` or `<thinking>` blocks. The `thinkTagFilter` is a stateful streaming filter that:

- Tracks whether the current position is inside a think block.
- Buffers partial tag matches across streaming chunks (a single `<think>` tag may be split across multiple events).
- Strips all content between open and close tags.
- Returns only the non-think content to the user.

This must be stateful because regex doesn't work on streaming chunks where tags span multiple events.

### Context Compaction

When the conversation approaches 80% of the context window, the `Compactor` (from `pkg/session`) triggers:

1. A `BeforeModelCallback` checks token usage before each LLM call.
2. If over threshold, it invokes an LLM-based summarization of the conversation history.
3. The summary replaces the full history, preserving key facts and recent tool results.
4. Fallback: if summarization fails, truncation removes oldest messages.

### Memory Reflection

After each turn with 3+ tool calls, the `MemoryReflector` runs a silent post-task LLM call:

1. Feeds the execution trace (tool calls, results, errors) to a specialized prompt.
2. The LLM decides whether durable knowledge was discovered (workarounds, non-obvious patterns, API quirks).
3. If yes, it calls `memory_save` to persist the knowledge.
4. This is the "insurance" layer -- the system prompt already instructs the LLM to save knowledge during execution, but the reflector catches anything it missed.

### Execution Tracing

Every user turn creates an `ExecutionTrace` that records:

- The user's request text
- Each tool call: name, args, result, success/failure, duration
- Sub-agent traces (nested, for `delegate_tasks`)
- The LLM's final text output

Traces serve two purposes:
1. **On-demand distillation**: The `/distill` command converts traces into reusable YAML flows.
2. **Memory reflection**: The reflector analyzes traces for knowledge worth persisting.

Traces are stored in-memory per session (max 20 per session) and can be reconstructed from persisted session events across daemon restarts.

## Key Files

| File | Purpose |
|---|---|
| `pkg/agent/chat_agent.go` | ChatAgent struct definition, fields, image/flow output side-channels |
| `pkg/agent/chat_agent_run.go` | ChatAgent.Run() -- the main execution loop with all phases |
| `pkg/agent/astonish_agent.go` | AstonishAgent struct, flow state machine, approval handling |
| `pkg/agent/node_llm.go` | LLM node execution for flows, callback wiring, retry logic |
| `pkg/agent/sub_agent.go` | SubAgentManager, SubAgentTask, tool groups, concurrent delegation |
| `pkg/agent/system_prompt_builder.go` | Three-tier system prompt construction |
| `pkg/agent/tool_index.go` | Hybrid vector+BM25 tool discovery index |
| `pkg/agent/tool_categories.go` | Safe (read-only) vs protected (write/exec) tool classification |
| `pkg/agent/execution_trace.go` | Execution trace recording for distillation and reflection |
| `pkg/agent/chat_distill.go` | Trace reconstruction from session events, distill preview/confirm |
| `pkg/agent/flow_distiller.go` | LLM-powered trace-to-YAML flow conversion |
| `pkg/agent/memory_reflection.go` | Post-task knowledge extraction via silent LLM call |
| `pkg/agent/think_filter.go` | Streaming chain-of-thought tag stripping |
| `pkg/agent/error_recovery.go` | LLM-powered error analysis and retry decisions (flows) |
| `pkg/agent/ephemeral_knowledge.go` | BeforeModelCallback for non-persisted knowledge injection |
| `pkg/agent/guidance_content.go` | Guidance documents for indexed retrieval (Tier 2) |
| `pkg/agent/tool_response_truncate.go` | BeforeModelCallback to truncate oversized tool responses |
| `pkg/agent/protected_tool.go` | Approval-gated tool wrapper |
| `pkg/agent/lazy_mcp_toolset.go` | Deferred MCP toolset initialization |

## Interactions

- **Sandbox**: Tools execute inside containers via the node protocol. The ChatAgent receives sandbox-wrapped tools from `WrapToolsWithNode()`.
- **Credentials**: BeforeToolCallback substitutes `{{CREDENTIAL:...}}` placeholders; AfterToolCallback restores them. PendingVault handles `<<<SECRET_N>>>` tokens.
- **Sessions**: The `SessionService` persists conversation history. Context compaction rewrites history when the window fills.
- **Memory**: Auto-knowledge retrieval queries the vector store before each turn. Memory reflection saves knowledge after turns.
- **Flows**: The `FlowDistiller` converts execution traces into YAML flow definitions. `AstonishAgent` executes those flows.
- **Channels**: Channel-specific hints are injected into the system prompt. Image side-channels deliver screenshots to Telegram/email.
- **Fleet**: Fleet agents use `SubAgentManager` with custom prompts, override tools, and dedicated sandbox containers.
