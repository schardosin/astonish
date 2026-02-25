---
name: git
description: "Advanced git operations — rebase, bisect, stash, worktree, reflog, cherry-pick"
require_bins: ["git"]
---

# Git (Advanced)

Basic operations (add, commit, push, pull, log, diff) are assumed knowledge.
This skill covers advanced operations that benefit from reference.

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
