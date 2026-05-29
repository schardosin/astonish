# Multi-File ClawHub Skill Support

**Status:** Draft  
**Date:** 2026-05-29  
**Author:** Astonish Team

## Problem Statement

Astonish currently supports ClawHub skills only as single-file `SKILL.md` documents stored in the database. Many real-world ClawHub skills (and the OpenClaw reference implementation) use a multi-file directory structure:

- `SKILL.md` (required)
- `scripts/` — helper scripts the agent can execute
- `references/` — documentation the agent can read on demand
- `assets/` — templates and resources

Current limitations:
- Only the `SKILL.md` content is stored (in the `skills.content` column).
- Auxiliary files from ClawHub ZIP downloads are discarded.
- The agent has no way to discover or load additional files belonging to a skill.
- This blocks adoption of the majority of useful community skills on ClawHub.

## Goals

1. **Full ClawHub compatibility** — Support the standard multi-file skill layout used by OpenClaw/ClawHub.
2. **Preserve the `skill_lookup` model** — Keep explicit, controllable loading via tools instead of eager injection.
3. **Strong isolation** — Skill files must never require host-to-container bind mounts when sandbox is enabled.
4. **Centralized storage** — All skill content lives in the platform database (works for both SQLite and PostgreSQL in multi-instance K8s deployments).
5. **Security by default** — Size limits, no arbitrary binary execution, clear provenance.

## Non-Goals (for v1)

- Executing pre-compiled binaries shipped inside skills (`bin/`).
- Full dependency/install-spec resolution (`metadata.openclaw.install`).
- Per-skill configuration/env injection.
- Skill publishing or verification flows.

## Design

### 1. Database Schema

We will use **Option A** (separate table) for minimal disruption:

#### New Table: `skill_files`

```sql
CREATE TABLE skill_files (
    id              TEXT PRIMARY KEY,
    skill_id        TEXT NOT NULL REFERENCES skills(id) ON DELETE CASCADE,
    path            TEXT NOT NULL,           -- relative directory, e.g. "scripts" or ""
    filename        TEXT NOT NULL,           -- e.g. "helper.sh" or "SKILL.md"
    content         TEXT NOT NULL,           -- file contents (text only)
    is_executable   BOOLEAN NOT NULL DEFAULT false,
    size_bytes      INTEGER NOT NULL,
    created_at      TEXT NOT NULL,
    updated_at      TEXT NOT NULL,

    UNIQUE(skill_id, path, filename)
);
```

**Notes:**
- `path` is the directory portion (empty string for root).
- `filename` is just the basename.
- Only text files are supported in v1 (`content` is `TEXT`).
- Binary files are rejected during import.
- The existing `skills` table remains unchanged (it continues to store the canonical `SKILL.md` in the `content` column for backwards compatibility and fast lookup).

Equivalent tables are created in both SQLite (per-team + org) and PostgreSQL schemas.

### 2. Tool Interface

#### `skill_lookup` (enhanced)

Existing signature continues to work:
```json
{ "name": "docker" }
```

New parameters for multi-file access:
```json
{
  "name": "docker",
  "file": "scripts/deploy.sh"     // or
  "path": "scripts",
  "filename": "deploy.sh"
}
```

**Revised behavior (inspired by Hermes progressive disclosure):**

- If only `name` is provided → returns the main `SKILL.md` **plus** a `files` manifest showing all available auxiliary files for that skill (no separate discovery call needed).
- If `file` (or `path`+`filename`) is provided → returns that specific file from `skill_files`.

Example response when loading the skill root:

```json
{
  "name": "docker",
  "description": "Container management with Docker",
  "content": "# Docker\n\n## Building Images\n...",
  "files": {
    "scripts": ["scripts/deploy.sh", "scripts/cleanup.sh"],
    "references": ["references/best-practices.md"],
    "templates": ["templates/Dockerfile.tmpl"]
  }
}
```

This approach reduces tool calls and ensures the model immediately knows what additional files exist for the skill.

### 3. System Prompt Instructions

The skill index section (already present via `BuildSkillIndex`) will be extended with guidance:

> **Multi-file skills**: Many skills contain additional files (`scripts/`, `references/`, `templates/`, etc.).
>
> When you call `skill_lookup(name)` on a skill, the response includes a `files` manifest listing all available auxiliary files. Use `skill_lookup(name, file: "scripts/foo.sh")` (or `path` + `filename`) to load any specific file.
>
> Relative paths mentioned inside a skill's `SKILL.md` must be resolved by loading them via `skill_lookup`.
>
> Never attempt to execute or reference files from a skill unless you first loaded them through `skill_lookup`.

This design (inspired by Hermes) ensures the model discovers auxiliary files in the same call that loads the main skill content, reducing unnecessary tool calls.

### 4. ClawHub Install Flow

When a user runs `astonish skills install <slug>` (or the future Studio equivalent):

1. Download the ZIP from the ClawHub registry endpoint.
2. Extract the archive in memory.
3. Locate `SKILL.md` (required).
4. Parse frontmatter (name, description, metadata).
5. Insert/update the main row in `skills` (name + full `SKILL.md` content).
6. For every other file in the archive:
   - If it is a text file and ≤ 256 KiB → insert into `skill_files`.
   - If binary or too large → log a warning and skip (with clear message to the user).
7. Store ClawHub provenance (slug, version, installed_at) — either in an extended `_meta` column or a small side table (future).

The same logic will apply to manual uploads via the Studio UI.

### 5. Runtime Execution Model (Sandbox vs Non-Sandbox)

**When sandbox is enabled (the common case in platform deployments):**

- `skill_lookup` runs on the host (it queries the DB).
- When the agent calls `skill_lookup(name)`, it receives both the SKILL.md content **and** the `files` manifest in one response.
- To load a specific auxiliary file, the agent calls `skill_lookup(name, file: "...")`.
- The tool returns the content.
- The agent uses `write_file` to place the content inside the session container at a well-known temporary location (e.g. `/tmp/skills/<skill-name>/scripts/deploy.sh`).
- The agent then uses `shell_command` (which executes inside the container) to run it.

**Benefits:**
- No host paths are ever leaked into the container.
- Full isolation is preserved.
- The agent is in control of what gets materialized inside the sandbox.

**When sandbox is disabled:**
- The agent can still use `write_file` + `shell_command`, or the system can optionally write files to a temp directory on the host for convenience. The tool behavior remains the same.

### 6. Limits and Security

- **Per-file limit**: 256 KiB (matching OpenClaw's `SKILL.md` cap). Larger files are rejected at import time.
- **Total per skill**: 2 MiB (soft limit, configurable later).
- **Content type**: Only UTF-8 text files are accepted. Binary content is rejected.
- **No execution of skill-shipped binaries**: Explicitly unsupported in v1 (and likely forever). Only scripts the agent writes into the container via `write_file` may be executed.
- **Provenance**: All imported files carry metadata about their origin (ClawHub slug + version, or manual upload user + timestamp).

## 7. UI Impact and Changes

The current Studio Skills UI (`web/src/components/settings/SkillsSettings.tsx`) is designed exclusively around single-file skills. It presents each skill as a single CodeMirror editor showing the full `raw_file` (YAML frontmatter + markdown body). There is no concept of auxiliary files in the UI today, even though the backend already sends a `has_directory` flag (currently ignored by the frontend).

Supporting multi-file ClawHub skills requires a significant but manageable evolution of the editor experience.

### 7.1 Current UI State (Single-File Only)

- Skill list shows name + description + eligibility badge.
- Clicking View/Edit opens a full-screen CodeMirror editor for the entire `SKILL.md`.
- Create flow: only asks for skill name → generates template → opens editor.
- No file tree, no tabs, no "Add File" capability.
- ClawHub installation is CLI-only (a hint message suggests using `astonish skills install`).

### 7.2 Target Editor Experience (Multi-File)

When a skill has auxiliary files, the editor should evolve to a two-pane layout:

```
┌────────────────────────────────────────────────────────────┐
│  ← Back to Skills                              [Save All]  │
├──────────────────────┬─────────────────────────────────────┤
│ Files                │  SKILL.md  │  scripts/deploy.sh    │
│                      ├─────────────────────────────────────┤
│ ▼ my-skill           │                                     │
│   SKILL.md        ●  │  ---                                │
│   ▼ scripts          │  name: my-skill                     │
│     deploy.sh        │  ...                                │
│     cleanup.sh       │                                     │
│   ▼ references       │  # My Skill                         │
│     api.md           │  Instructions...                    │
│                      │                                     │
│ [+ Add File]         │                                     │
│                      │                                     │
└──────────────────────┴─────────────────────────────────────┘
```

**Key behaviors:**

- **Single-file skills** (only `SKILL.md`): Keep the current full-screen editor experience (no tree panel) for backwards compatibility.
- **Multi-file skills**: Show a collapsible file tree on the left.
- Clicking any file opens it in a tab on the right (CodeMirror with appropriate language mode: markdown, shell, etc.).
- **[+ Add File]** button allows creating new files under `scripts/`, `references/`, `templates/`, or custom paths.
- Right-click / hover menu on files supports Rename and Delete (with confirmation; `SKILL.md` cannot be deleted).
- Files in `scripts/` are visually marked as executable.
- Changes to any file are tracked; a global "Save All" button saves modified files.

### 7.3 API Changes Required

The existing skill content API must be extended:

| Method | Endpoint | Purpose |
|--------|----------|---------|
| `GET`  | `/api/skills/{name}/content?scope=` | Extend response to include `files: [{path, filename, size, is_executable}]` |
| `GET`  | `/api/skills/{name}/files` | List all auxiliary files for a skill |
| `GET`  | `/api/skills/{name}/files/{path}/{filename}` | Retrieve content of one auxiliary file |
| `PUT`  | `/api/skills/{name}/files/{path}/{filename}` | Create or overwrite an auxiliary file |
| `DELETE` | `/api/skills/{name}/files/{path}/{filename}` | Delete an auxiliary file |

New backend handlers will be needed in `pkg/api/skills_handlers.go` to support the file operations (especially for the platform DB stores).

### 7.4 Frontend Component Changes

Major updates expected in:

- `SkillsSettings.tsx` — Core editor view will need significant refactoring to support the two-pane + tabbed layout.
- New component: `SkillFileTree.tsx` — Renders the left-side file explorer.
- New or extended editor wrapper to manage multiple open tabs and dirty state across files.
- API client functions in `web/src/api/` or `settingsApi.ts` for the new file endpoints.

### 7.5 Create Flow

The "New Skill" modal can remain simple (just name). After creation:
- The editor opens showing only `SKILL.md`.
- The user can immediately use `[+ Add File]` to start building the multi-file structure.

No need to overcomplicate the initial creation step.

### 7.6 ClawHub Install UI (Phase 3)

A future "Install from ClawHub" button in the skills list header can:
- Accept a ClawHub slug or URL.
- Call a new `POST /api/skills/install` endpoint.
- Show the number of files imported and open the skill in the new multi-file editor.

This is lower priority; the CLI path works well in the meantime.

### 7.7 Backwards Compatibility

- Skills that only contain `SKILL.md` continue to render exactly as today.
- The file tree panel only appears when the `files` array returned by the API is non-empty (or when `has_directory` is true).
- Existing single-file skills require zero changes from users.

## Implementation Phases

### Phase 1: Foundation (DB + Tools)

- Add `skill_files` table (and equivalent org/team versions) + migrations.
- Extend `skill_lookup` so that calling it with only `name` returns the main content + a `files` manifest of auxiliary files.
- Extend `skill_lookup` to accept `file` / `path`+`filename` parameters to fetch specific files from `skill_files`.
- Update system prompt instructions to reflect the single-tool progressive disclosure model.
- Add basic validation + size limits at import time.

### Phase 2: ClawHub Import

- Modify `DownloadFromClawHub` + CLI install path to populate `skill_files`.
- Add support for importing multi-file ZIPs via the existing `skills install` command.
- Update tests.

### Phase 3: Agent Experience & Polish (including Studio UI)

- Finalize the `files` manifest format returned by `skill_lookup(name)`.
- Add clear error messages when a skill references a file that doesn't exist in the manifest.
- Implement full multi-file support in the Studio Skills editor:
  - File tree sidebar + tabbed CodeMirror editor
  - Add / Rename / Delete file operations
  - Support for executable flag on scripts
- Extend backend skill content APIs to return file manifests and support file CRUD.
- Update documentation and bundled skill examples if needed.
- Ensure `skill_lookup` (with file support) is included in the default tool allowlist.

### Phase 4: Future (out of scope for v1)

- ClawHub lockfile / version tracking
- `skills update` command
- ClawHub install UI in Studio (Phase 3 has the editor; full install flow is future)
- Install-spec evaluation (`metadata.openclaw.install`)
- Per-skill configuration
- Binary file support (explicitly out of scope)

## Open Questions

- Should we allow the agent to request a "bulk load" of small files in one call, or keep it strictly one-file-at-a-time via repeated `skill_lookup` calls?
- Do we want a size-based heuristic that automatically inlines very small auxiliary files (< 2 KiB) directly in the `skill_lookup` response?
- How (if at all) do we want to surface the existence of auxiliary files in the lightweight skill index shown in the system prompt (vs. only revealing them when the model calls `skill_lookup` on the skill)?

## References

- Existing skills architecture: `docs/architecture/skills.md`
- OpenClaw skill layout and loading: `/root/openclaw` (reference implementation)
- Hermes agent skills system: `/root/hermes-agent` — particularly its progressive disclosure model using `skills_list` + `skill_view(name)` (which returns a `linked_files` manifest) + `skill_view(name, file_path)`. This directly influenced the decision to embed the file manifest in the primary `skill_lookup` response instead of using a separate `skill_tree` tool.
- Current `skill_lookup` implementation: `pkg/tools/skill_lookup.go`
- ClawHub download logic: `pkg/skills/clawhub.go`
- Skill store interfaces: `pkg/store/skills.go`

---

**Next step:** Once this document is approved, we will begin Phase 1 implementation.
