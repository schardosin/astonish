# pkg/fleet — AGENTS.md

Multi-agent orchestration: fleets are reusable templates that describe a graph of agents (roles + delegations + tools) and route work between them, optionally hooked into external channels (GitHub, Slack).

## Scope
- Fleet configuration model (`config.go`): `FleetConfig`, `FleetAgentConfig`, `FleetSettings`, `ToolsConfig`, `DelegateConfig`, capabilities/execution/memory/task_policy.
- Session lifecycle (`session_manager.go`, `dispatcher.go`): `FleetSession` with serial + bounded parallel activation.
- Durable runtime (`recovery.go` + team stores): `FleetRunState`, mailbox, task board via `pkg/store` interfaces.
- Plan activation and registry (`plan_activator.go`, `plan_registry.go`): `PlanActivator`, `PlanRegistry`, `PlanSummary`, `SchedulerJob`.
- Channel bridges (`channel.go`, `channel_github.go`, `github_reporter.go`): outbound reporting into channel adapters.
- `Message` — the internal envelope posted to Channel for SSE/transcript; durable per-recipient mailbox is the agent-facing source of truth for handoffs.
- `monitor_state.go` — GitHub poll cursor only; not session run-state. Do not conflate with `FleetRunState`.
- Bundled templates (`bundled/*.yaml`, `IsBundledKey`) — Astonish-shipped, immutable; custom templates live in the team DB under non-bundled keys.
- Setup profiles (`setup_profile.go`, `setup_engine.go`, `setup_prompt.go`, `setup_tool_catalog.go`, `setup_step_presets.go`, `bundled/setup-profiles/`) — reusable plan-creation steps decoupled from templates. Templates reference profiles via `setup_profile:`; do not embed new `plan_wizard` blocks.
- **Steps are the source of truth.** Each step carries `prompt`, optional `content`, `tools`, and `pinned_tool_groups`. Never add profile-level `wizard_prompt` prose — put instructions on steps. Chat and the form wizard share the same step model; chat is scoped to the current incomplete step until `update_setup_draft` validates.

## Key ideas
- A **fleet** is a static description (YAML/schema) of agents + their allowed delegations. A **fleet session** is a live run of a fleet.
- Plans are activated (`PlanActivator`) either by user action or by `SchedulerJob` (see `pkg/scheduler/AGENTS.md` — the scheduler triggers activation on a cron tick).
- The GitHub channel adapter (`channel_github.go`, `github_reporter.go`) posts progress/results to GitHub issues; do not couple fleet logic to any specific channel — keep the channel-agnostic path through `pkg/channels`.
- **Serial regression floor:** when `MaxParallelAgents ≤ 1`, runtime uses serial activation with durable mailbox + always-on task board (no shared_channel mode).
- **Parallel dispatch:** when `MaxParallelAgents ≥ 2` and agents are `execution.parallelizable`, the dispatcher may activate a fan-out batch concurrently. Bundled `software-dev` is serial (`max_parallel_agents: 1`); custom templates may enable parallel fan-out.
- **Bundled templates are immutable.** Embedded keys always win on GET/LIST. PUT/DELETE of a bundled key must fail (`store.ErrBundledTemplateImmutable` / HTTP 409). Customize via clone to a new key stored in the team DB. Same-key DB orphan rows are left in place but ignored (no boot-time migration).
- **`CapabilityRegistry` is advisory and domain-neutral.** The editor surfaces generic capability hints; templates may declare any free-form capability name (e.g. `code.write`, `genetics.analysis`). Only `supervisor` has special meaning when `routing_mode: supervisor`.
- **`EnsureBundled` is legacy** (file-based personal mode only). Platform mode (Postgres or SQLite team DB) must not overwrite user data via that helper.

## When editing
1. Changing the fleet YAML schema? Update `FleetConfig` and validation, then regenerate any docs and update `pkg/daemon/run.go` where fleets are loaded.
2. Adding a new channel bridge? Prefer implementing the generic `Channel` interface in `pkg/channels` and consuming it from `pkg/fleet` — do not add channel-specific code here.
3. Changing `PlanActivator`? Coordinate with `pkg/scheduler` — the scheduler's `DeliverFunc` targets fleet plans.
4. New durable tables must be team-scoped via `ent/team/schema` + `pkg/store` interfaces + `entstore` impls; never bypass the tenant router.
5. Do not change the `Channel` interface or `FleetRecoverFunc` signature for mailbox/recovery — layer alongside / wrap instead.
6. Never allow Save/Delete to mutate a bundled template key; always prefer clone → custom key.
