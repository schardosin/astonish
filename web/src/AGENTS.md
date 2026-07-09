# web/src — AGENTS.md

React 19 SPA for Astonish Studio. Mixed **TypeScript (`.ts`/`.tsx`)** and **legacy JSX (`.jsx`)** — new files should be `.tsx`.

## Scope
- Entry: `main.tsx` (React root) and `App.tsx`.
- `components/` — React components (mix of `.tsx` and `.jsx`).
- `api/` — SPA fetch/SSE client for the Go `/api/*` surface.
- `sandbox-runtime.ts` — sandboxed iframe runtime for Apps (generative UI).

## Conventions
- **Language**: `.tsx` for new UI files unless mirroring an existing `.jsx` neighbor. `npm run build` runs `tsc --noEmit` — do not commit code that fails it.
- **Components**: functional + hooks, single per file, `export default` for the main component.
- **State**: React hooks only. **No Redux/Zustand/Jotai/etc.** Cross-cutting state uses React Context sparingly.
- **Styling**: Tailwind CSS v4 with `var(--variable-name)` theming (`index.css`).
- **Imports**: external first, local second. Named exports preferred for utilities.
- **Handlers**: CamelCase, prevent default on forms, cleanup in `useEffect`.
- **Lint**: ESLint in `web/eslint.config.js`. Separate blocks for `{js,jsx}` and `{ts,tsx}` — do not merge them.
- **Testing**: Vitest (`npm test`).

## Non-negotiable invariants

### StudioChat.tsx (report vs. artifact rendering)
`StudioChat.tsx` implements the client side of the **three-signal report gate**. A markdown artifact renders inline (`EmbeddedFileViewer`) **only** when all three signals hold:

1. Emitted in the last turn (after the most recent user message).
2. `fileType === 'Markdown'`.
3. `isReport === true` (set by the backend when an `` ```astonish-report `` fence's `path:` matched the artifact path).

Anything failing any one of these renders as the compact `ArtifactCard`. **Do not widen the gate in the SPA.** If the backend regresses the marker, fix it in `pkg/api/chat_runner.go`, not by weakening the client check.

Authoritative reference: `docs/architecture/chat-rendering-pipeline.md`, "The Report Pipeline" section.

### Generative UI (Apps) sandbox
The Apps runtime runs user-described React apps in a sandboxed iframe with an opaque origin, communicating with the parent via `postMessage` and a SSRF-protected server-side proxy. Do not remove the iframe boundary or the origin isolation.

Authoritative reference: `docs/architecture/generative-ui.md`.

### SSE consumer contract
Every SSE event type produced by `pkg/api/chat_runner.go` has a matching handler in `StudioChat.tsx` (and related components). If you add an event type on the backend, add its handler here and add a scenario fixture. If you rename an event type, update both sides in the same commit.

## When editing
1. Adding a new component? `.tsx`, functional + hooks, keep it under 300 lines. Extract if it grows.
2. Adding a new SSE event? See the "SSE consumer contract" above.
3. Adding a new page? Register it in the router, wire up any needed API call in `api/`, and cover it with a Vitest test.
