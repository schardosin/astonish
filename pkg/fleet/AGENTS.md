# pkg/fleet — AGENTS.md

Multi-agent orchestration: fleets are reusable templates that describe a graph of agents (roles + delegations + tools) and route work between them, optionally hooked into external channels (GitHub, Slack).

## Scope
- Fleet configuration model (`config.go`): `FleetConfig`, `FleetAgentConfig`, `ToolsConfig`, `DelegateConfig`.
- Session lifecycle (`session_manager.go`): `FleetSession`.
- Plan activation and registry (`plan_activator.go`, `plan_registry.go`): `PlanActivator`, `PlanRegistry`, `PlanSummary`, `SchedulerJob`.
- Channel bridges (`channel.go`, `channel_github.go`, `github_reporter.go`): outbound reporting into channel adapters.
- `Message` — the internal envelope passed between fleet agents.

## Key ideas
- A **fleet** is a static description (YAML/schema) of agents + their allowed delegations. A **fleet session** is a live run of a fleet.
- Plans are activated (`PlanActivator`) either by user action or by `SchedulerJob` (see `pkg/scheduler/AGENTS.md` — the scheduler triggers activation on a cron tick).
- The GitHub channel adapter (`channel_github.go`, `github_reporter.go`) posts progress/results to GitHub issues; do not couple fleet logic to any specific channel — keep the channel-agnostic path through `pkg/channels`.

## When editing
1. Changing the fleet YAML schema? Update `FleetConfig` and validation, then regenerate any docs and update `pkg/daemon/run.go` where fleets are loaded.
2. Adding a new channel bridge? Prefer implementing the generic `Channel` interface in `pkg/channels` and consuming it from `pkg/fleet` — do not add channel-specific code here.
3. Changing `PlanActivator`? Coordinate with `pkg/scheduler` — the scheduler's `DeliverFunc` targets fleet plans.
