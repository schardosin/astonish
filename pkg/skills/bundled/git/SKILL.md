---
name: git
description: "Git operations — clone, shallow clone, sparse checkout, rebase, bisect, stash, worktree, reflog, cherry-pick"
require_bins: ["git"]
---

# Git

Basic operations (add, commit, push, pull, log, diff) are assumed knowledge.
This skill covers cloning strategies and advanced operations that benefit from reference.

## Cloning & Fetching
```
git clone <url>                          # Full clone
git clone --depth 1 <url>               # Shallow clone (latest commit only — fast)
git clone --filter=blob:none <url>      # Blobless clone (full history, lazy file fetches)
git clone --sparse <url>                # Sparse clone (checkout only root)
```
After sparse clone, use `git sparse-checkout` to select specific directories:
```
git sparse-checkout set src/ pkg/       # Only check out src/ and pkg/
git sparse-checkout add docs/           # Add another directory
```

**When to clone:** For any task that requires reading, analyzing, or comparing multiple files from a repository, clone it locally first. This gives you full access to `grep_search`, `file_tree`, `read_file`, and the complete codebase — far more efficient than fetching files one-by-one via HTTP.

**If clone fails:** Do NOT immediately abandon cloning. Follow this fallback chain:
1. **Retry once** — DNS and network errors are often transient, especially in sandboxed environments.
2. **Try `gh repo clone`** — the GitHub CLI uses different auth and transport paths that may succeed when `git clone` fails. Load the `github` skill if needed.
3. **Diagnose** — run `nslookup github.com` or `curl -I https://github.com` to check connectivity before concluding the network is unavailable.
4. **Try `web_fetch`** on `raw.githubusercontent.com/<owner>/<repo>/main/<path>` — lightweight HTTP fetch, no MCP server needed.
5. **Only as a last resort**, fall back to MCP scraping tools (e.g., firecrawl). These are slow, truncation-prone, and produce dramatically worse results than a local clone.

Never conclude "network is unavailable" from a single failed clone attempt.

## Interactive Rebase
```
git rebase -i HEAD~3                    # Rebase last 3 commits
git rebase -i main                      # Rebase onto main
git rebase --abort                      # Cancel a rebase in progress
git rebase --continue                   # Continue after resolving conflicts
```
Rebase commands: `pick`, `reword` (change message), `edit` (amend), `squash` (merge into previous), `fixup` (squash, discard message), `drop` (remove).

## Stash
```
git stash                               # Stash uncommitted changes
git stash push -m "description"         # Stash with message
git stash list                          # List stashes
git stash pop                           # Apply and remove latest stash
git stash apply stash@{2}              # Apply specific stash (keep it)
git stash drop stash@{0}               # Remove a stash
git stash show -p stash@{0}            # View stash diff
```

## Bisect (Find Bug-Introducing Commit)
```
git bisect start                        # Start bisecting
git bisect bad                          # Current commit is bad
git bisect good <commit>                # Known good commit
# Git checks out middle commit — test it, then:
git bisect good                         # This commit is good
git bisect bad                          # This commit is bad
# Repeat until the bad commit is found
git bisect reset                        # Exit bisect mode
```

## Cherry-Pick
```
git cherry-pick <commit>                # Apply a commit to current branch
git cherry-pick <start>..<end>          # Range of commits
git cherry-pick --no-commit <commit>    # Stage changes without committing
```

## Worktree (Multiple Working Directories)
```
git worktree add ../feature-branch feature  # Check out branch in new directory
git worktree list                           # List worktrees
git worktree remove ../feature-branch       # Remove a worktree
```

## Reflog (Recovery)
```
git reflog                              # Show recent HEAD movements
git reflog show <branch>                # Show branch reflog
git checkout HEAD@{5}                   # Go back to a reflog entry
git branch recovered HEAD@{5}           # Create branch from reflog entry
```

## Tags
```
git tag v1.0.0                          # Lightweight tag
git tag -a v1.0.0 -m "Release 1.0.0"  # Annotated tag
git push origin v1.0.0                  # Push specific tag
git push origin --tags                  # Push all tags
```

## Tips
- Use `git log --oneline --graph --all` for a visual branch overview
- Use `git diff --stat` for a summary of changes
- Use `git blame <file>` to find who changed each line
- Use `git clean -fd` to remove untracked files and directories
