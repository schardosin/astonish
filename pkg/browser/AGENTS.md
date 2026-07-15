# pkg/browser — AGENTS.md

Managed Chromium via Chrome DevTools Protocol (rod). 35+ browser tools in pure Go — no Node, no Playwright.

## Scope
- `manager.go` — `Manager`, `BrowserConfig`, `HandoffCfg`, `PageState`.
- `recording.go` — ffmpeg `x11grab` session recording (`StartRecording` / `StopRecording` / `RecordingStatus`).
- `handoff.go` — `HandoffServer`, `HandoffOpts`, `HandoffInfo`: human-in-the-loop handoff (user takes over a session in a real browser).
- `captcha.go` / `captcha_solver.go` — `CAPTCHADetection`, `CAPTCHASolveRequest`, `CAPTCHASolveResult`, `CAPTCHASolverConfig`.
- `accounts.go` — `Account`, `AccountStore` (cookies/credentials per site).

## Key rules
1. **Everything runs inside a sandbox** in Studio/platform mode. The browser package integrates with `pkg/sandbox` for containerized runs. Do not spin up a host-level Chrome outside the sandbox path.
2. **Handoff is opt-in per session**. When a handoff is active, agent-driven actions must pause — never race the user.
3. **Cookies and stored logins** live in `AccountStore` and cascade through the credential store's encryption. Do not persist them in plain files.
4. **Stealth mode toggles** live in `BrowserConfig` — do not sprinkle stealth patches throughout the code.
5. **Recording captures the real display** (KasmVNC/Xvfb) at launch-time resolution (default 1920×1080). Do not use CDP screencast. Wire container ffmpeg via `ContainerStartRecordingFunc` — browser must not import `pkg/sandbox`.

## When editing
- Adding a new browser tool? Register it in `pkg/tools` and route it through `Manager` — do not open a new rod page from the tool directly.
