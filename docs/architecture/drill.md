# Drill (Test Engine)

## Overview

Drill is Astonish's AI-composed, mechanically-replayed test engine. The agent creates test suites by analyzing an application, then the suites are executed deterministically with shell commands, browser interactions, and assertions -- without LLM involvement during execution. This separates the creative work (writing tests) from the mechanical work (running tests), ensuring reproducible results.

Drill suites are stored as YAML flow definitions (type: `drill_suite` and `drill`) and executed by a specialized runner that handles multi-service orchestration, readiness checks, assertion evaluation, visual regression, and failure triage.

## Key Design Decisions

### Why Separate Composition from Execution

Most AI testing approaches have the LLM drive the browser in real-time (reasoning about what to click, what to assert). This is slow, non-deterministic, and expensive. Drill separates the two phases:

1. **Composition** (LLM-powered): The agent analyzes the application, identifies test scenarios, and generates YAML test definitions with explicit steps and assertions.
2. **Execution** (mechanical): The drill runner executes steps exactly as defined, evaluates assertions deterministically, and reports results without any LLM calls.

The only exception is `semantic` assertions and `triage` mode, which use targeted LLM calls for specific judgment tasks.

### Why Multi-Service Ready Checks

Real applications often require multiple services (database, backend, frontend). Drill suites support:

- **HTTP ready checks**: Poll a URL until it returns 200 (e.g., `http://localhost:3000/health`).
- **Port ready checks**: Wait for a TCP port to accept connections.
- **Output ready checks**: Wait for a specific string in the service's stdout (e.g., "Server started on port").

Services are started in declaration order and stopped in reverse order. Each service can have its own ready check and environment variables.

### Why Visual Regression Testing

Drill includes pixel-level visual comparison:

1. First run creates baseline screenshots.
2. Subsequent runs compare against baselines with configurable tolerance (default 1%).
3. Anti-aliasing tolerance ignores pixel differences in border regions.
4. Red-highlighted diff images are generated for visual inspection.
5. Baselines are stored as artifacts and can be manually updated.

### Why LLM-Powered Triage

When a drill fails in `triage` mode, an LLM analyzes the failure context and classifies it as:

- **Transient**: Timing issue, network flake -- retry likely to succeed.
- **Bug**: Genuine application defect -- needs developer attention.
- **Environment**: Infrastructure problem -- not related to the application.
- **Test issue**: The test itself is wrong -- needs updating.

This automated triage reduces the human effort needed to process test failures.

## Architecture

### Suite Lifecycle

```
1. Setup Phase:
   - Create sandbox container from template
   - Start services in order (setup commands)
   - Wait for each service's ready check
   - Resolve container IP for browser access
    |
    v
2. Test Execution Phase:
   For each drill in the suite:
     - Run test steps (shell commands or browser interactions)
     - Evaluate assertions after each step
     - On failure: stop, continue, or triage (configurable)
     - Collect artifacts (logs, screenshots, diff images)
    |
    v
3. Teardown Phase:
   - Stop services in reverse order (teardown commands)
   - Collect final artifacts
   - Generate JSON report
```

### Assertion Types

| Type | Description | Source |
|---|---|---|
| `contains` | Output contains expected string | stdout, snapshot, screenshot |
| `not_contains` | Output does not contain string | stdout, snapshot, screenshot |
| `regex` | Output matches regular expression | stdout, snapshot, screenshot |
| `exit_code` | Command exited with expected code | shell command |
| `element_exists` | CSS selector finds element in DOM | browser page |
| `semantic` | LLM judges if output meets criteria | any (uses LLM call) |
| `visual_match` | Screenshot matches baseline within threshold | browser screenshot |

### Drill YAML Structure

```yaml
# Suite definition (type: drill_suite)
description: "E-commerce smoke tests"
type: drill_suite
suite_config:
  template: "@myapp"
  services:
    - name: database
      setup: "docker compose up -d postgres"
      ready_check:
        type: port
        port: 5432
        timeout: 30
    - name: backend
      setup: "npm start"
      ready_check:
        type: http
        url: "http://localhost:3000/health"
  teardown:
    - "docker compose down"

# Individual drill (type: drill)
description: "User can add item to cart"
type: drill
suite: "E-commerce smoke tests"
drill_config:
  timeout: 120
  on_fail: triage
nodes:
  - name: navigate_to_product
    type: tool
    action: browser_navigate
    args:
      url: "http://localhost:3000/products/1"

  - name: check_product_page
    type: tool
    action: browser_snapshot
    assert:
      type: contains
      expected: "Add to Cart"

  - name: add_to_cart
    type: tool
    action: browser_click
    args:
      ref: "add-to-cart-btn"

  - name: verify_cart
    type: tool
    action: browser_snapshot
    assert:
      type: semantic
      expected: "Cart shows 1 item with the correct product"
```

### Composite Executor

The drill runner uses a composite executor that routes different tool categories to different backends:

- **Container tools** (shell_command, file ops): Routed to the sandbox container via NDJSON.
- **Browser tools**: Routed to the host browser (with container IP for URL resolution).
- **Local tools**: Direct in-process execution for tools that don't need sandbox or browser.

This handles the common case where a drill starts services in a container but tests them via a browser on the host.

### Parameterized Tests

Drills support data-driven testing via the `parameters` field:

```yaml
parameters:
  - username: "admin"
    password: "secret123"
    expected: "Welcome, Admin"
  - username: "guest"
    password: "guest"
    expected: "Welcome, Guest"
```

Each parameter set runs the full drill with those values substituted into `{{variable}}` placeholders in node args.

### Artifact Management

The drill runner collects:

- **Logs**: stdout/stderr from each step.
- **Screenshots**: Captured at assertion points and on failure.
- **Diff images**: Red-highlighted pixel differences for visual regression failures.
- **JSON report**: Structured test results with timings, assertion outcomes, and triage classifications.

## Key Files

| File | Purpose |
|---|---|
| `pkg/drill/runner.go` | Main drill runner: suite lifecycle, test execution, assertion evaluation |
| `pkg/drill/assertions.go` | Assertion evaluation logic for all types |
| `pkg/drill/visual.go` | Visual regression: baseline management, pixel comparison, diff generation |
| `pkg/drill/triage.go` | LLM-powered failure classification |
| `pkg/drill/artifacts.go` | Artifact collection and management |
| `pkg/drill/executor.go` | Composite executor: routes tools to sandbox, browser, or local |
| `pkg/config/yaml_loader.go` | Drill YAML schema: DrillSuiteConfig, DrillConfig, AssertConfig |
| `pkg/tools/drill_tool.go` | Drill management tools (save, validate, delete, list) |
| `pkg/tools/run_drill_tool.go` | Drill execution tool with the 1000-line creation wizard prompt |

## Interactions

- **Sandbox**: Drill services run inside containers. The composite executor routes container tools through the sandbox node protocol.
- **Browser**: Browser-based drills use the host browser pointed at the container's IP address.
- **Flows**: Drills are a specialized flow type (`type: drill`/`drill_suite`) stored in the flow directory.
- **Fleet**: The E2E agent in a fleet can run drills as part of its validation workflow.
- **API/Studio**: Drill view in Studio provides suite management, execution, and result viewing.
