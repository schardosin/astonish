# E2E Scenario Coverage Catalog

This directory is the **source of truth** for E2E test scenarios — real user journeys exercised against a running Astonish platform with real database, real server, and real LLM.

**This catalog does NOT track unit tests or frontend Vitest tests.** Those have their own coverage tools (`npm run test:coverage`, `go test -cover`).

## Quick Start

```bash
make scenario-coverage
```

## File Layout

```
tests/scenarios/
├── chat.yaml       # Chat view E2E scenarios
├── flows.yaml      # Flow Canvas E2E scenarios
├── fleet.yaml      # Fleet view E2E scenarios
├── drill.yaml      # Drill view E2E scenarios
├── apps.yaml       # Apps view E2E scenarios
├── report.mjs      # Reconciler script
└── README.md       # This file
```

## Scenario Entry Format

```yaml
- id: FLOWS-001                         # Stable ID — never reused
  title: "Create a flow via AI..."      # One-line summary (≤80 chars)
  user_story: |                         # What the user does and observes
    As a user, I describe a flow...
  priority: P0                          # P0=must-have, P1=important, P2=nice-to-have
  status: covered                       # covered | missing
  test_refs:                            # Pointer to the actual Go test
    - "tests/e2e/flow_assistant/flow_assistant_test.go::TestE2E_FlowAssistant_CreateFlow"
```

## Marking a Test as Covering a Scenario

Add a `COVERS:` comment in the Go test file:

```go
// COVERS: FLOWS-001
func TestE2E_FlowAssistant_CreateFlow(t *testing.T) { ... }
```

The report greps for these and cross-checks against the catalog.

## Adding New Scenarios

1. Pick the correct view file
2. Assign the next available ID (e.g., `CHAT-016`)
3. Write the `user_story` from the user's perspective
4. Set `priority` and `status: missing`
5. Implement the test under `tests/e2e/`, add `// COVERS: <ID>`
6. Flip `status: covered`, add `test_refs`
7. Run `make scenario-coverage` to verify

## Rules

- IDs are immutable — never reuse or renumber
- User stories describe behavior, not implementation (no component names, no SSE event types)
- Every scenario requires a real backend to be meaningful — if it can be tested with mocked fetch, it's a unit test, not an E2E scenario
- P0 promotion requires product sign-off
