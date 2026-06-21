# Generative UI

Generative UI lets you describe an application in plain English and get a live, interactive React app rendered directly in your chat session. No boilerplate, no build step, no deployment — just describe what you want and use it immediately.

## How It Works

1. **You describe** — Tell the agent what you need in natural language.
2. **Agent generates JSX** — The AI writes a React component with Tailwind CSS styling.
3. **Instant rendering** — Sucrase compiles the JSX to JavaScript in ~5ms and renders it in a secure sandboxed iframe.
4. **Iterate** — Refine with follow-up messages until it's exactly right.
5. **Save** — Promote the result to a named App that persists and stays accessible.

```
You:    "Build me a dashboard showing my team's sprint velocity over
         the last 6 sprints with a bar chart and current sprint progress"
Agent:  [generating app] I'll create a sprint dashboard with velocity
        tracking and progress indicators.

        [renders live interactive dashboard in chat]

        Here's your sprint dashboard. It shows velocity as a bar chart
        and current sprint progress as a radial gauge. Want me to
        connect it to your Jira data?
```

<!-- IMAGE: screenshot showing a rendered dashboard app in the Studio chat -->

## What's Available

Generated apps have access to pre-bundled libraries — no installation needed:

| Library | Purpose |
|---------|---------|
| React 19 | Component framework |
| Recharts | Charts and data visualization |
| Lucide React | Icon library |
| Tailwind CSS v4 | Utility-first styling |

For data connectivity, apps use [data hooks](./data-hooks.md) — `useAppData` for fetching, `useAppAction` for mutations, `useAppAI` for LLM calls, and `useAppState` for persistent storage — all backend-proxied so credentials never reach the browser.

## Comparison to Alternatives

| | Astonish Generative UI | Claude Artifacts | ChatGPT Canvas |
|---|---|---|---|
| Live data connectivity | Yes (MCP, REST, OAuth) | No | No |
| Persistent state | Yes (per-user DB) | No | No |
| Team sharing | Yes (publish/fork) | Share link | Share link |
| LLM calls from app | Yes (useAppAI) | No | No |
| Iterative refinement | Yes | Yes | Yes |
| Saved as first-class concept | Yes (Apps) | Artifacts | No |

## Apps as a Top-Level Concept

Generated UIs are saved as **Apps** — a first-class entity in Astonish alongside Flows and Fleet agents. Apps appear in the Studio sidebar, can be pinned for quick access, and maintain their own persistent state per user.

## The Compilation Pipeline

Astonish uses [Sucrase](https://github.com/alangpierce/sucrase) for JSX transformation instead of Babel or a full bundler. This gives:

- **~5ms compilation** — No perceptible delay between generation and rendering.
- **No Node.js dependency** — The transform runs in the browser.
- **Hot updates** — Each refinement recompiles and re-renders instantly.

The rendered app runs in a sandboxed iframe with restricted permissions. Network requests are proxied through the Astonish backend, ensuring credentials and API keys never reach the client.

## Getting Started

- [Building Apps](./building-apps.md) — Step-by-step guide to creating apps
- [Data Hooks](./data-hooks.md) — Connect apps to live data
- [Sharing & Persistence](./sharing.md) — Team publishing and state management
