# pkg/skills — AGENTS.md

`SKILL.md` loader, validator, and [ClawHub](https://clawhub.com) integration. Skills teach the agent about tools, CLIs, and workflows via markdown files.

## Scope
- `loader.go` — `Skill`, `ParseSkillFile`, `IsEligible`.
- `validator.go` — `ValidationResult`, `ValidationIssue`, `ValidatorConfig`.
- `clawhub.go` — `ClawHubMeta`, `InstallResult`.

## Key rules
1. **A skill is a `SKILL.md` file** — no code, no hidden files. The whole contract is markdown parsing + eligibility rules.
2. **Eligibility is deterministic**: `IsEligible` must return the same result for the same session state. Do not add nondeterministic checks.
3. **Team scoping (platform mode)**: skills cascade `platform → org → team → personal`. Do not pull team skills into a personal context.
4. **ClawHub metadata is normalized before use** — trust the normalized shape, not the raw metadata.

## When editing
- Adding a new skill front-matter field? Extend `Skill`, update the validator, and document the field in the docs site.
- Changing eligibility logic? Coordinate with the prompt builder in `pkg/agent` — skills appear in the system prompt.
