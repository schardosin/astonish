# Testing Chat Scenarios

> **DEPRECATED** — This document is superseded by the YAML scenario catalog at
> `tests/scenarios/` (one file per view: `chat.yaml`, `flows.yaml`, `fleet.yaml`,
> `drill.yaml`, `apps.yaml`). Use `make scenario-coverage` to see current coverage.
> New scenarios go in the YAML catalog, not here.

## Why this stub still exists

The original document was a 1,800-line catalog of chat scenarios labelled `A1` through `W4` plus backend scenarios `X1` through `X9`. Those labels appear in older commits, PR descriptions, and code comments. This stub preserves the label-to-area mapping so that historical references remain locatable, without continuing to maintain a parallel catalog that duplicates the YAML source of truth.

If you arrived here from an old reference, find the area below and consult the matching YAML file (`tests/scenarios/<view>.yaml`) for the current authoritative scenarios. IDs in the YAML are stable and indexed by `make scenario-coverage`.

## Historical section index

| Old label | Area | Modern home |
|-----------|------|-------------|
| A | Core chat flow (Q&A, streaming, sessions, abort) | `tests/scenarios/chat.yaml` Section A + `web/src/test/scenarios/core-chat.test.tsx` |
| B | Tool execution (single, parallel, expand, image, auto-approval, artifacts) | `tests/scenarios/chat.yaml` (tool sections) + `web/src/test/scenarios/tool-execution.test.tsx` |
| C | Tool approval flow | `web/src/test/scenarios/tool-interactions.test.tsx` |
| D | Task delegation (`delegate_tasks`) | `web/src/test/scenarios/task-delegation.test.tsx` |
| E | Plan announcement & tracking | `web/src/test/scenarios/plan-tracking.test.tsx` |
| F | Plan + delegation combined | `plan-tracking.test.tsx` + `task-delegation.test.tsx` |
| G | App preview / generative UI | `web/src/test/scenarios/app-preview.test.tsx` + `docs/architecture/generative-ui.md` |
| H | App preview iframe communication | `docs/architecture/generative-ui.md` |
| I | Error handling | `web/src/test/scenarios/error-handling.test.tsx` |
| J | Distill flow | `web/src/test/scenarios/distill-flow.test.tsx` |
| K | Download & export (DOCX, PDF, markdown) | `tests/scenarios/chat.yaml` Section H (export) + `tests/e2e/chat_core/chat_export_test.go` |
| L | Artifact & file management | `tests/scenarios/chat.yaml` (artifact entries) + `web/src/test/scenarios/downloads-artifacts.test.tsx` |
| M | ResultCard | `docs/architecture/chat-rendering-pipeline.md` ("The Report Pipeline") |
| N | Panel management (Todo / Files / Apps mutual exclusion) | `web/src/test/scenarios/panel-interactions.test.tsx`, `panel-management.test.tsx` |
| O | Fleet mode | `tests/scenarios/fleet.yaml` + `web/src/test/scenarios/fleet-mode.test.tsx` |
| P | Browser handoff | `web/src/test/scenarios/browser-handoff.test.tsx` |
| Q | Thinking & system messages | `web/src/test/scenarios/misc-messages.test.tsx` |
| R | Slash commands | `web/src/test/scenarios/slash-commands.test.tsx` |
| S | Session management | `web/src/test/scenarios/session-management.test.tsx`, `session-interactions.test.tsx` |
| T | Clipboard operations | `web/src/test/scenarios/clipboard-copy.test.tsx` |
| U | Mermaid diagrams | `web/src/test/scenarios/misc-messages.test.tsx` |
| V | Usage tracking | `web/src/test/scenarios/misc-messages.test.tsx` |
| W | Wizard flows (fleet-plan, drill, drill-add) | `tests/scenarios/fleet.yaml`, `tests/scenarios/drill.yaml` |
| X1–X9 | Backend integration (ChatRunner pipeline) | `pkg/api/integration_scenarios_test.go`, `pkg/api/integration_expanded_test.go` |

## See also

- `docs/TESTING.md` — comprehensive layered test reference
- `docs/architecture/testing.md` — build-tag taxonomy, e2e harness, decision tree
- `docs/architecture/chat-rendering-pipeline.md` — authoritative chat rendering contract (read before modifying `StudioChat.tsx`)
- `tests/scenarios/README.md` — how to add YAML scenarios and the `// COVERS:` rule
