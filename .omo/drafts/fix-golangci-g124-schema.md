---
slug: fix-golangci-g124-schema
status: approved
intent: clear
review_required: false
pending-action: execute .omo/plans/fix-golangci-g124-schema.md
approach: delete the non-existent G124 gosec exclude from .golangci.yml
---

# Draft: fix-golangci-g124-schema

## Components (topology ledger)
| id | outcome | status | evidence path |
|----|---------|--------|--------------|
| C1 | `.golangci.yml` passes schema validation in golangci-lint v2.11.4 | active | .omo/evidence/task-1-fix-golangci-g124-schema.txt |

## Open assumptions (announced defaults)
| assumption | adopted default | rationale | reversible? |
|------------|----------------|-----------|-------------|
| No nolint comments reference G124 | verified by grep — none found | grep of full repo returned no matches | N/A — factual |
| G124 has no corresponding gosec check | confirmed — valid enum jumps G123→G201 | golangci-lint CI error enum listing is authoritative | N/A — factual |

## Findings (cited - path:lines)
- `.golangci.yml:79-80` — only occurrence of G124 in repo; `# Secure cookie flag is set dynamically…` comment + `- G124` exclude
- CI error message: `"linters.settings.gosec.excludes.17" does not validate … value must be one of 'G101'..'G123', 'G201'..` — G124 not in enum
- Index 17 (0-based) in the excludes list = entry #18 = `G124` (last line of gosec section)
- golangci-lint workflow uses v2.11.4 which performs strict schema validation

## Decisions (with rationale)
1. **Delete the G124 entry entirely** — it references a non-existent rule, so removing it causes zero behaviour change. No workaround (comment, skip, version pin) needed.
2. **Do not pin to an older golangci-lint** — the schema validation is a feature, not a regression; we fix the config, not the tool.

## Scope IN
- Delete lines 79–80 of `.golangci.yml` (comment + `- G124` line)

## Scope OUT (Must NOT have)
- Do NOT downgrade golangci-lint version in lint.yml
- Do NOT add `//nolint` comments as workaround
- Do NOT touch any other gosec exclude
- Do NOT modify any Go source files

## Open questions
None — all resolved by investigation.

## Approval gate
status: approved
user confirmed "go"
