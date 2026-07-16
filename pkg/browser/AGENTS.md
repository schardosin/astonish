# pkg/browser — AGENTS.md

Managed Chromium via Chrome DevTools Protocol (rod). 35+ browser tools in pure Go — no Node, no Playwright.

## Scope
- `manager.go` — `Manager`, `BrowserConfig`, `HandoffCfg`, `PageState`.
- `recording.go` — ffmpeg `x11grab` session recording (`StartRecording` / `StopRecording` / `RecordingStatus`); probes live X size via xdpyinfo.
- `demo_overlay.go` — tutorial highlight overlays, visible demo cursor (`EnableDemoCursor` / `MoveMouseAnimated`), `SetFullscreen`.
- `action_recorder.go` — DOM action capture (`StartActionCapture` / `StopActionCapture` / `GetActionLog`) for tutorial authoring; works under KasmVNC because listeners run in-page.
- `handoff.go` — `HandoffServer`, `HandoffOpts`, `HandoffInfo`: human-in-the-loop handoff (user takes over a session in a real browser).
- `captcha.go` / `captcha_solver.go` — `CAPTCHADetection`, `CAPTCHASolveRequest`, `CAPTCHASolveResult`, `CAPTCHASolverConfig`.
- `accounts.go` — `Account`, `AccountStore` (cookies/credentials per site).

## Key rules
1. **Everything runs inside a sandbox** in Studio/platform mode. The browser package integrates with `pkg/sandbox` for containerized runs. Do not spin up a host-level Chrome outside the sandbox path.
2. **Handoff is opt-in per session**. When a handoff is active, agent-driven actions must pause — never race the user. `capture_actions` on `browser_request_human` starts DOM capture for the handoff window and stops on Done (`StopHandoff`).
3. **Cookies and stored logins** live in `AccountStore` and cascade through the credential store's encryption. Do not persist them in plain files.
4. **Stealth mode toggles** live in `BrowserConfig` — do not sprinkle stealth patches throughout the code.
5. **Recording captures the live X display** (probed size). Keep KasmVNC `allow_resize: false` **and** set `desktop.resolution` / `-geometry` to the viewport (default 1920×1080) — locking resize without an explicit resolution freezes at the package default 1024×768. Do not use CDP screencast. Wire container ffmpeg via `ContainerStartRecordingFunc` — browser must not import `pkg/sandbox`.
6. **Action capture is in-page instrumentation**, not CDP Input tapping — required so KasmVNC mouse events are recorded.

## When editing
- Adding a new browser tool? Register it in `pkg/tools` and route it through `Manager` — do not open a new rod page from the tool directly.
