---
title: PR Description Generator
description: Build a practical agent that generates PR descriptions
---

# PR Description Generator

In this tutorial, you'll build a real-world agent that reads GitHub PRs and generates descriptions.

## What We're Building

An agent that:
1. Lists open PRs in a repository
2. Lets you select one
3. Fetches the diff
4. Generates a professional PR description

## Prerequisites

- Astonish installed and configured
- `gh` CLI installed and authenticated ([GitHub CLI](https://cli.github.com/))

## The Flow YAML

Here's the complete agent:

```yaml
name: pr_description_generator
description: Generate PR descriptions from code changes

nodes:
  - name: get_prs
    type: llm
    prompt: List open PRs in the current repository using gh CLI
    tools: true
    tools_selection: [shell_command]
    output_model:
      prs: str

  - name: select_pr
    type: input
    prompt: "Select a PR number from:\n{prs}"
    output_model:
      pr_number: int

  - name: get_diff
    type: llm
    prompt: Get the diff for PR #{pr_number} using gh CLI
    tools: true
    tools_selection: [shell_command]
    output_model:
      diff: str

  - name: generate_description
    type: llm
    system: You are a technical writer who creates clear, professional PR descriptions.
    prompt: |
      Generate a clear PR description for this diff.
      Include:
      - Summary of changes
      - Key modifications
      - Any breaking changes
      
      Diff:
      {diff}
    output_model:
      description: str
    user_message:
      - description

flow:
  - from: START
    to: get_prs
  - from: get_prs
    to: select_pr
  - from: select_pr
    to: get_diff
  - from: get_diff
    to: generate_description
  - from: generate_description
    to: END
```

## Step-by-Step Breakdown

### 1. List PRs

The first node uses the `shell_command` tool to run `gh pr list`:

```yaml
- name: get_prs
  type: llm
  prompt: List open PRs in the current repository using gh CLI
  tools: true
  tools_selection: [shell_command]
  output_model:
    prs: str
```

The LLM figures out to run `gh pr list` and captures the output.

### 2. User Selection

An input node pauses for user input:

```yaml
- name: select_pr
  type: input
  prompt: "Select a PR number from:\n{prs}"
  output_model:
    pr_number: int
```

The `{prs}` variable is populated from the previous node.

### 3. Get the Diff

Another LLM node fetches the diff:

```yaml
- name: get_diff
  type: llm
  prompt: Get the diff for PR #{pr_number} using gh CLI
  tools: true
  tools_selection: [shell_command]
  output_model:
    diff: str
```

### 4. Generate Description

Finally, we generate the description:

```yaml
- name: generate_description
  type: llm
  system: You are a technical writer...
  prompt: |
    Generate a clear PR description...
    {diff}
  user_message:
    - description  # Display this to the user
```

## Running It

```bash
# Navigate to a repo with open PRs
cd /path/to/your/repo

# Run the agent
astonish agents run pr_description_generator
```

## Output Example

```
🤖 Fetching open PRs...
   Running: gh pr list --state open

📋 Open PRs:
   #42 - Add user authentication
   #38 - Fix database connection pooling
   #35 - Update dependencies

Select a PR number: 42

📥 Fetching diff for PR #42...

✍️ Generated Description:

## Summary
This PR implements user authentication using JWT tokens.

## Key Changes
- Added `AuthMiddleware` for route protection
- Implemented login/logout endpoints
- Added password hashing with bcrypt

## Breaking Changes
None - this is additive functionality.
```

## Next Steps

- Modify the prompt for your team's PR template
- Add a node to automatically update the PR description
- Integrate with your code review process
