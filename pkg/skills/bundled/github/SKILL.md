---
name: github
description: "GitHub operations via the gh CLI — PRs, issues, releases, workflows, gists"
require_bins: ["gh"]
---

# GitHub CLI (gh)

## When to Use
- Managing pull requests (create, review, merge, list)
- Working with issues (create, close, label, assign)
- Checking CI/CD workflow status and logs
- Creating and managing releases
- Managing gists
- Repository operations (clone, fork, view)

## When NOT to Use
- Direct GitHub API calls (use web_fetch with api.github.com instead)
- Large file operations or LFS (use git directly)

## Authentication
`gh` uses `GITHUB_TOKEN` or browser-based OAuth. Check auth status with `gh auth status`.

## Common Commands

### Pull Requests
```
gh pr list                              # List open PRs
gh pr list --state all --limit 20       # All PRs including closed
gh pr view <number>                     # View PR details
gh pr create --title "..." --body "..." # Create PR from current branch
gh pr create --fill                     # Auto-fill title/body from commits
gh pr checkout <number>                 # Check out a PR branch locally
gh pr merge <number> --squash           # Squash merge a PR
gh pr review <number> --approve         # Approve a PR
gh pr diff <number>                     # View PR diff
gh pr checks <number>                   # View CI check status
```

### Issues
```
gh issue list                           # List open issues
gh issue create --title "..." --body "..."  # Create issue
gh issue close <number>                 # Close issue
gh issue view <number>                  # View issue details
gh issue edit <number> --add-label bug  # Add label
```

### Workflows & CI
```
gh run list                             # List recent workflow runs
gh run view <run-id>                    # View run details
gh run view <run-id> --log              # View run logs
gh run watch <run-id>                   # Watch a run in progress
gh workflow list                        # List workflows
gh workflow run <workflow> -f key=value # Trigger workflow
```

### Releases
```
gh release list                         # List releases
gh release create v1.0.0 --generate-notes  # Create release with auto notes
gh release create v1.0.0 ./dist/*       # Create release with assets
```

### Repository
```
gh repo view                            # View current repo info
gh repo clone owner/repo                # Clone a repo
gh repo fork                            # Fork current repo
```

## Tips
- Use `--json` flag for machine-readable output: `gh pr list --json number,title,state`
- Use `--jq` for filtering JSON output: `gh pr list --json number,title --jq '.[].title'`
- Most commands auto-detect the current repository from git remote
