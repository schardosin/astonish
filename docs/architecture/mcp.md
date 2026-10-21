# MCP Integration

> The public inbound MCP contract is documented in [MCP Public Tool Loop](mcp-public-loop.md). It intentionally exposes only `get_agent_context`, `search_tools`, `describe_tools`, and `execute_tool`; `astonish_chat` is removed with no compatibility alias. External clients must treat skills and tool names returned by this endpoint as Astonish capabilities: they load matching skills and invoke the tools those skills recommend through Astonish's discovery and execution operations rather than probing or substituting tools in the client environment.

## Overview

Astonish integrates with the Model Context Protocol (MCP) to extend the agent's capabilities with external tool servers. MCP servers are standalone processes that expose tools via a JSON-RPC protocol over stdio. Astonish manages the lifecycle of these servers, caches their tool definitions, and optionally runs them inside sandbox containers for security.

## Key Design Decisions

### Why MCP

MCP provides a standardized way to extend AI agents with new tools without modifying the agent itself. The ecosystem of MCP servers is growing rapidly, covering GitHub, databases, APIs, file systems, and more. By supporting MCP, Astonish gains access to this ecosystem.

### Why Sandboxed MCP Transport

MCP servers are arbitrary executables that could access the host filesystem, network, and credentials. Running them inside sandbox containers provides:

- **Isolation**: An MCP server can't access host files or network.
- **Reproducibility**: The same container environment across machines.
- **Security**: Even a malicious MCP server is contained.

The `ContainerMCPTransport` implements the MCP SDK's `Transport` interface by starting the server process inside a container via `ExecNonInteractive` and bridging stdin/stdout to the MCP JSON-RPC connection.

### Why Separate Stderr

MCP uses JSON-RPC over stdout. If an MCP server writes log messages to stdout (a common mistake), it corrupts the JSON-RPC stream. The `ExecNonInteractive` call uses `SeparateStderr: true` to keep stderr separate, and the captured stderr is available for diagnostics.

MCP startup and discovery failures must be logged with enough context to diagnose cloud deployments from container logs alone: server name, transport, command and args for stdio servers, URL scheme/host/path for remote servers, env var names, the underlying error, and captured stderr. The sandbox stdio transport also captures the non-JSON stdout lines it discards before forwarding the stream to the MCP SDK. That keeps JSON-RPC messages private to the protocol while still exposing package-manager or runtime failures that incorrectly print to stdout before an `initialize` EOF. Env var values and URL credentials/query strings are intentionally omitted from logs.

### Why Tool Caching

Querying MCP servers for their tool definitions involves starting the server process, performing the JSON-RPC handshake, and listing tools. This can take seconds. The tool cache:

- Persists tool definitions to disk (`~/.config/astonish/tools_cache.json`).
- Refreshes in the background so the agent always has a warm cache.
- Allows the agent to know what MCP tools are available without waiting for server startup.

### Why a MCP Store

The MCP Store provides a curated catalog of MCP servers with pre-built configurations. Users can browse available servers, install them with one click, and the correct command, args, and environment variables are configured automatically. The store data is embedded in the binary as JSON.

## Architecture

### MCP Server Lifecycle

```
Configuration (config.yaml or Studio):
  mcp_servers:
    github:
      command: npx
      args: ["-y", "@modelcontextprotocol/server-github"]
      env:
        GITHUB_TOKEN: "{{CREDENTIAL:github:token}}"
    |
    v
Daemon startup:
  1. Parse MCP server configs
  2. Load tool cache from disk
  3. For each server: start in background, list tools, update cache
  4. Register tools with the agent via LazyMCPToolset
    |
    v
Tool call from agent:
  1. Agent calls an MCP tool (e.g., "github_create_issue")
  2. LazyMCPToolset starts the MCP server if not running
  3. JSON-RPC call: {"method": "tools/call", "params": {...}}
  4. Response returned to agent
    |
    v
Shutdown:
  - MCP server processes are terminated
```

### Sandboxed MCP Execution

```
Host:
  Agent requests MCP tool call
    |
    v
  LazyNodeClient.EnsureContainerReady() -- Phase 1 only (no node process needed)
    |
    v
Container:
  ContainerMCPTransport.Connect():
    1. ExecNonInteractive(command, args, env, SeparateStderr=true)
    2. Filter stdout so only JSON-RPC lines reach mcp.IOTransport
    3. Capture discarded non-JSON stdout for diagnostics
    4. Return mcp.Connection
    |
    v
  JSON-RPC over stdio:
    Host stdin -> Container process stdin
    Container process stdout -> Host stdout
```

### LazyMCPToolset

The `LazyMCPToolset` defers MCP server startup until tools are actually needed:

- At agent creation time, tool definitions are loaded from cache (fast).
- On first tool call, the MCP server is started and the JSON-RPC connection is established.
- This avoids starting servers that may never be used in a session.

### Tool Surfacing Through the Fixed Bridge

MCP tools are catalog entries in every runtime, including Astonish Code. Each
server is registered as an `mcp:<server>` tool group. The model finds tools via
catalog-only `search_tools`, inspects schemas with `describe_tools`, and invokes
them through the fixed `execute_tool` bridge, or delegates parallel work with the
corresponding group. MCP discovery never adds provider-visible declarations.

### Config-File MCP Servers as the Platform Base Layer

MCP server resolution mirrors provider resolution: servers declared in the local
config file are the **base layer**, and the platform database cascades on top.

- **Personal mode / Astonish Code**: `config.LoadMCPConfig()` reads
  `~/.config/astonish/mcp_config.json` and merges the standard web/model servers
  (Tavily, Brave, Firecrawl, Perplexity) declared under `web_servers` in
  `config.yaml`. No platform login is required — a config-file MCP server is
  immediately available to the agent.
- **Platform mode**: the two platform-mode loaders —
  `loadMCPConfigForRequest` (`pkg/api/request_helpers.go`) and `loadMCPConfig`
  (`pkg/launcher/chat_factory.go`) — seed the merge map from
  `config.FileMCPServers()` **first** (Tier 0), then overlay the database tiers
  (platform → org → team), then merge standard servers. This lets an operator
  ship an installation with default MCP servers declared in `mcp_config.json`
  that are visible to every org/team, exactly like config-file providers become
  platform defaults.

Cascade order (later tiers override earlier ones by server name):

```
config file (mcp_config.json)   ← FileMCPServers(), the base layer
  → platform DB (MCPServerStore, scope=platform)
    → org DB (MCPServerStore, scope=org)
      → team DB (MCPServerStore, scope=team)
        → standard servers (config.yaml web_servers, layered last)
```

`config.FileMCPServers()` intentionally excludes standard-server IDs — those are
layered separately by `MergeStandardServersWithConfig` using the effective
(DB-resolved) `AppConfig` so team-level `WebSearchTool` selection is honored.

**Tool discovery for file servers.** File-declared servers have no per-tenant
`cached_tools` DB column, so their tool declarations live in the shared on-disk
tools cache (`~/.config/astonish/tools_cache.json`), the same cache standard
servers use. Discovery happens automatically — no manual "refresh" step is
required:

- **Standalone chat build (personal mode)**: when the chat factory
  (`pkg/launcher/chat_factory.go`) finds a config-file server with no cached
  tools, it starts the server on the host, lists its tools, and writes them to
  the file cache (`discoverAndCacheHostMCPTools`). Subsequent launches are
  instant.
- **Local platform install (daemon)**: at startup the daemon calls
  `api.DiscoverUncachedFileMCPServers`, which kicks off background discovery for
  every enabled config-file server that is not yet cached (respecting sandbox /
  network policy via `discoverMCPToolsForPlatform`). This is why config-file MCP
  servers "just work" after `astonish daemon restart`.
- **Manual refresh**: `RefreshMCPServerHandler` can refresh a file server only
  with explicit `scope=platform` and platform-superadmin authority, after an
  available platform store returns no row. Team/org/default requests, missing
  stores, and store errors never fall back to shared file discovery.

`cachedToolsForMCPServer` and the chat factory both fall back to this file cache
when the DB has no `cached_tools`.

**Advertised tool list.** `GetCachedToolsForRequest` (`pkg/api/tools_cache.go`)
builds the tool list the model actually *sees* — it feeds the flow-builder AI
system prompt and `GET /api/tools`. In platform mode it MUST also seed Tier 0
from `config.FileMCPServers()` (reading the file server's tool declarations from
the on-disk tools cache via `cache.GetToolsForServer`, honoring
`config.GetExcludedTools`), with DB tiers overriding by name — otherwise a
config-file server is connectable and shown in Settings but never advertised, so
the agent reports it doesn't exist. This is base-layer parity with the three
other platform-mode surfaces below.

**Platform-mode surfaces that must include the file base layer (Tier 0).** All
four seed the merge from `config.FileMCPServers()` before overlaying the DB
tiers, so the cascade above holds uniformly:

- `loadMCPConfigForRequest` (`pkg/api/request_helpers.go`) — the actual MCP
  connection config for a request.
- `loadMCPConfig` (`pkg/launcher/chat_factory.go`) — the chat factory MCP config.
- `GetCachedToolsForRequest` (`pkg/api/tools_cache.go`) — the advertised tool
  list (system prompt + `GET /api/tools`).
- `GetMCPServersHandler` (`pkg/api/handlers.go`) — the Settings list.

**Listing / UI.** `GetMCPServersHandler` seeds its list from
`config.FileMCPServers()` and marks those entries `source: "config"`. Such
entries are read-only defaults from the config file; an org/team can override a
file server by installing a same-named DB entry, which takes precedence in the
cascade above.

### MCP Write Scope Authorization

Individual MCP GET requests treat a nil row as not found, returning HTTP 404 rather than dereferencing it. Existing lookup-error handling and read-scope selection remain unchanged; populated reads still redact a copy without mutating stored secrets. Handler regressions cover nil rows, storage errors, error-plus-row results, populated responses, and unavailable stores across explicit and compatibility read scopes. This does not normalize the team store's missing-row error contract: modern team PUT of a nonexistent masked target can still return a bounded HTTP 500 before validation, while org/platform return 400. That inconsistency remains open and is not counted as authorization-denial evidence.

`resolveMCPStoreForWrite` in `pkg/api/mcp_platform_handlers.go` is the write
boundary for platform MCP CRUD/refresh and catalog, internet, and standard-server
installation/removal. Omitted scope means **team**. Only `team`, `org`, and
`platform` are writable; `effective` and unknown values return 400. Team writes
require team administration, org writes require org administration, and platform
writes require a platform superadmin. Missing selected stores return 503, never a
fallback to another scope. Install authorization precedes body parsing, catalog
fetches, persistence, and background discovery. The legacy toggle
(`PATCH /api/mcp/servers/{name}`) and refresh (`POST /api/mcp/{name}/refresh`)
use the same gate before body decoding or lookup; they do not use the merged
read resolver. This intentionally changes omitted legacy write scope to team.
The `/api/mcp-platform/servers/{name}` toggle and refresh handlers treat a
successful store lookup returning a nil row as 404, before mutation, discovery,
or chat reset. This does not broaden the legacy refresh handler's explicit,
superadmin-only file-server fallback described above.

Local authenticated HTTP tests use temporary SQLite and test-issued JWTs to
exercise role/scope rejection and successful scoped toggles. File-refresh handler
tests suppress discovery and verify selection/cache snapshots only. These are not
proof of live MCP execution. Name-only discovery deduplication and global chat
reset remain separate runtime-isolation obligations; this write-scope repair does
not claim tenant-isolated discovery caches or invalidation.

Standard-server env values stay in the selected MCP store. Only an explicitly
platform-scoped superadmin operation may write/delete the daemon-wide
`web_servers.<id>.api_key` secret or delete its legacy file-secret fallback.
Narrow-scope uninstall must preserve inherited platform credentials. A failed
standard-server MCP-store deletion returns 500 before credential cleanup, web-tool
settings changes, or chat reset; it must not report a successful uninstall while
the server record remains.

After successful deletion, explicit `scope=platform` uninstall clears matching
`WebSearchTool` and `WebExtractTool` references in `PlatformSettings` only; it
never falls back to team settings or mutates team references, even matching ones.
Other scopes retain the existing team `Settings` routing, matching install (not
new org-settings routing). Each reference is cleared only if it still matches the
removed server's tool; replacement references and unrelated settings fields are
preserved. Settings cleanup remains best-effort, not an atomic transaction with
MCP deletion or credential cleanup.

Local handler regressions in `standard_servers_handler_test.go` exercise scoped
install/uninstall against both the platform secret adapter and an isolated legacy
encrypted credential file, plus rejection before body reads or store mutations for
invalid/read-only scopes and missing selected stores. Successful-install tests
suppress discovery through its existing in-flight deduplication guard; they do not
prove live sandbox discovery or deployed middleware behavior.

### MCP Response Redaction and Masked Updates

Platform MCP list/get responses redact response copies, not stored runtime
configuration. Every nonempty environment literal is replaced with `****`, even
for keys without a secret-looking name. A complete `{{CREDENTIAL:name:field}}`
reference remains visible; mixed literal/reference strings are masked. Empty
values remain empty. URLs use the shared fail-closed URL redactor: userinfo,
nonempty query values (including repeated/arbitrarily named parameters), and
fragments are masked; malformed or opaque URLs are masked in full.

`restoreMCPServerSecrets` validates proposed writes before Save or discovery.
Callers must supply an existing record read from the **exact authorized write
store and name**, never a merged/inherited view. Creates cannot use masks.
Environment masks must match an existing nonempty literal at the same key; URL
masks must exactly equal the stored URL's redacted view. Successful URL restoration
preserves original bytes, including signatures, encodings, and query order.
Restoration is rejected if name, command, arguments, transport, or destination
changes: callers must replace or clear retained credentials instead of silently
sending existing secrets to another executable or endpoint. Errors do not include
secret values and leave inputs unchanged.

Platform PUT requests preserve omitted/null fields; explicit empty strings/maps
clear their fields. Omitted credentials receive the same destination checks as
explicit masks. Existing response/request field names remain unchanged. Settings
bulk/inline consumers can reuse `redactMCPEnv` and `restoreMCPServerSecrets`; they
must resolve URL omission to the stored **redacted** URL before validation and
must validate the complete batch before persisting any item.

### MCP Inspector

Studio provides an MCP Inspector that allows:

- Viewing all registered MCP servers and their status.
- Listing available tools from each server.
- Testing individual tools with custom inputs.
- Viewing server logs and errors.

For stdio servers, the inspector uses the same sandbox rule as chat and app data: the server process never runs on the Astonish host. Tool listing uses a disposable `mcp-discover-*` sandbox; tool invocation uses the per-user app sandbox. Before a stdio package manager such as `npx` or `uvx` starts, persisted Astonish Network Policy allow rules are pre-seeded into the OpenShell sandbox policy.

The same pre-seeding requirement applies to background MCP discovery after install, refresh, standard-server install, and MCP Store install. Those jobs intentionally detach from the HTTP request context so they can continue after the response, but they must carry a detached runtime context containing the Network Policy stores and OpenShell gateway config. Without that detached runtime context, a Settings allow rule can exist in the database but never reach the disposable `mcp-discover-*` sandbox.

If sandboxed MCP startup fails with network-looking diagnostics, the inspector response includes `network_authorization`. The payload contains parsed denied endpoints from the MCP process output and package-manager preflight hints such as `registry.npmjs.org:443` for npm-based servers. The Studio UI shows these endpoints and lets an admin explicitly approve a durable network policy rule for the selected MCP scope. The retry then creates or reuses a fresh sandbox with the persisted allow rule pre-seeded. Astonish does not silently whitelist hosts.

## Key Files

| File | Purpose |
|---|---|
| `pkg/mcp/manager.go` | MCP server lifecycle management |
| `pkg/sandbox/mcp_transport.go` | ContainerMCPTransport: sandboxed MCP execution |
| `pkg/agent/lazy_mcp_toolset.go` | LazyMCPToolset: deferred MCP server startup |
| `pkg/cache/tools_cache.go` | Persistent tool definition cache |
| `pkg/config/mcp_config.go` | MCP server configuration |
| `pkg/config/standard_servers.go` | Standard MCP server definitions |
| `pkg/mcpstore/` | MCP server catalog with embedded data |
| `pkg/api/mcp_handlers.go` | MCP management API endpoints |
| `pkg/api/mcp_inspector.go` | Studio MCP Inspector tool-list and run handlers |
| `pkg/api/mcp_network_grants.go` | Durable MCP network allow-rule approval endpoint |
| `pkg/api/mcp_diagnostics.go` | Secret-safe MCP diagnostics and network authorization payloads |

## Interactions

- **Agent Engine**: MCP tools are registered alongside built-in tools. The ToolIndex indexes MCP tool names for semantic discovery.
- **Sandbox**: MCP servers can run inside containers via ContainerMCPTransport. The LazyNodeClient provides the container.
- **Configuration**: MCP servers are configured in `config.yaml` or via the Studio UI. Servers declared in the local `mcp_config.json` file are the base layer in both personal and platform mode: in platform mode they are visible to every org/team as defaults and can be overridden by a same-named database entry (config file → platform DB → org DB → team DB → standard servers). See "Config-File MCP Servers as the Platform Base Layer" above.
- **Credentials**: MCP server environment variables can reference secrets from the credential store via `{{CREDENTIAL:name:field}}` placeholders (e.g. `GITHUB_TOKEN: "{{CREDENTIAL:github:token}}"`). Placeholders are stored in config/DB and expanded only when the MCP process starts (host, sandbox, discovery, inspector). The Studio Editor binds env keys via a credential picker / create-secret flow; the Source view shows the raw placeholder form. Blank sensitive env values such as `TOKEN`, `KEY`, `SECRET`, `PASSWORD`, or `AUTH` keys are omitted during install so `NAME=""` does not shadow a real process environment variable.
- **Flows**: Flow MCP dependencies declare which MCP servers a flow requires.
- **API/Studio**: MCP endpoints manage servers, the inspector provides debugging.
