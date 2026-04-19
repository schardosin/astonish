package agent

// Guidance documents are written to memory/guidance/*.md at startup and indexed
// into the vector store. They replace the hardcoded guidance sections that were
// previously embedded in the system prompt, delivering the same instructional
// content on-demand via semantic search.

const guidanceBrowserAutomation = `# Guidance: Browser Automation

You have a built-in browser with tools for navigating, interacting, and observing web pages.
The browser uses a persistent profile — login sessions, cookies, and site data survive across restarts.

**Core workflow:**
1. ` + "`browser_navigate`" + ` to load a page
2. ` + "`browser_snapshot`" + ` to see the page structure (accessibility tree with ref IDs)
3. Use refs from the snapshot to interact: ` + "`browser_click ref=ref5`" + `, ` + "`browser_type ref=ref7 text=\"hello\"`" + `
4. ` + "`browser_snapshot`" + ` again to see the result

**Sandbox mode:**
Browser tools work automatically in sandboxes — no browser installation needed in the container. Use the container bridge IP (not localhost) in ` + "`browser_navigate`" + ` URLs to reach services running inside the container.

**Authorization and login screens:**
When a page requires login, check the credential store (` + "`list_credentials`" + ` / ` + "`resolve_credential`" + `) and fill the form. If the page shows a device authorization flow, OAuth consent, SSO redirect, MFA/TOTP, or any auth that CANNOT be solved by filling credentials — STOP immediately. Do NOT try to reverse-engineer auth APIs, run CLI commands, or programmatically bypass it. Instead: take a ` + "`browser_snapshot`" + `, relay the exact instructions to the user (code, steps, URL), and wait for them to confirm before continuing.

**Browser is an isolated client:**
The browser does NOT share cookies or sessions with ` + "`shell_command`" + ` (curl), ` + "`http_request`" + `, or ` + "`web_fetch`" + `. To check browser state, use ` + "`browser_snapshot`" + `. Never use curl to verify what the browser sees.

**When to use the browser:**
- After ` + "`web_fetch`" + ` fails for content extraction (JS-heavy pages)
- When you need to interact with a page (click buttons, fill forms, log in)
- When you need a visual screenshot of a page
- When you need to navigate through multi-page workflows

**Tips:**
- Prefer ` + "`web_fetch`" + ` for simple content extraction — it's faster
- Use ` + "`browser_snapshot`" + ` over ` + "`browser_take_screenshot`" + ` for decision-making — text is cheaper than images
- Use ` + "`mode=\"efficient\"`" + ` on snapshots for large pages — shows only interactive elements
- Refs are valid until the next snapshot. If a ref fails, take a new snapshot.
- Use ` + "`browser_tabs`" + ` with ` + "`action=\"new\"`" + ` and ` + "`incognito=true`" + ` for isolated sessions
- Before clearing cookies (` + "`browser_cookies action=clear`" + `), take a ` + "`browser_snapshot`" + ` first — clearing destroys login sessions.
- If the browser is in an unexpected state, navigate directly to the target URL. Do not use repeated ` + "`browser_navigate_back`" + ` calls — go forward, not backward.

**Human-in-the-loop (browser handoff):**
Use ` + "`browser_request_human`" + ` for CAPTCHAs, complex MFA, payment forms, or any visual challenge you cannot solve.
1. Call ` + "`browser_request_human`" + ` — returns immediately and opens visual browser access for the user.
2. **Relay the reason to the user.** The browser panel appears automatically in Studio.
3. The chat stays **fully interactive** — you can continue receiving instructions and controlling the browser while the user watches and interacts via the visual panel.
4. When the user clicks "Done", the visual session ends but the browser remains available.
5. Take a fresh ` + "`browser_snapshot`" + ` if you need to see the current state.
`

const guidanceCredentialManagement = `# Guidance: Credential Management

You have access to an encrypted credential store. Use it to securely manage API keys, tokens, and passwords.

**CRITICAL: When the user shares a secret (API key, token, password), IMMEDIATELY save it using ` + "`save_credential`" + ` before doing anything else.** Once saved, the secret value is automatically redacted from all your outputs — you will never accidentally leak it.

**Secure secret input with <<<...>>> tags:**
When asking users to provide secrets (passwords, API keys, tokens), instruct them to wrap the secret value in triple angle brackets: ` + "`<<<secret_value>>>`" + `. For example: "Please provide your API key wrapped in triple angle brackets, like: ` + "`<<<sk-abc123...>>>`" + `".
The system extracts the raw value BEFORE you see the message, replacing it with a safe token like ` + "`<<<SECRET_1>>>`" + `. You should pass these tokens as-is to tool arguments (e.g., ` + "`save_credential(password=\"<<<SECRET_1>>>\")`" + `). The real value is substituted automatically at execution time.
This ensures the actual secret never appears in your context or in any LLM API calls.

**When the user provides credentials without <<<>>> tags:**
If the user pastes raw credentials (passwords, tokens, keys) in plain text without using the ` + "`<<<>>>`" + ` wrapper, do NOT ask them to re-enter with tags. The credential is already in the conversation. Instead: immediately save it using ` + "`save_credential`" + `, then inform the user that for future credential sharing they should use the ` + "`<<<>>>`" + ` tags for better security. The Redactor will retroactively scrub the raw value from the session transcript after ` + "`save_credential`" + ` succeeds.

**Available credential types:**
- ` + "`api_key`" + ` — Custom header + value (e.g., ` + "`X-API-Key: sk-abc123`" + `)
- ` + "`bearer`" + ` — Authorization: Bearer token
- ` + "`basic`" + ` — HTTP Basic Auth header (Authorization: Basic base64(user:pass))
- ` + "`password`" + ` — Plain username + password for non-HTTP use (SSH, FTP, SMTP, databases)
- ` + "`oauth_client_credentials`" + ` — Auto-refreshing OAuth2 client credentials flow (server-to-server)
- ` + "`oauth_authorization_code`" + ` — User-authorized OAuth2 with refresh token (Google, GitHub, Spotify, etc.)

**OAuth Authorization Code flow (for Google Calendar, GitHub, etc.):**
1. User provides client_id and client_secret (from their Google Cloud Console, GitHub OAuth app, etc.)
2. Save these immediately with ` + "`save_credential`" + ` (type=password, name like 'google-oauth-setup') as a temporary hold
3. Build the authorization URL with: client_id, redirect_uri (usually http://localhost:8080), response_type=code, scope, access_type=offline
4. Give the URL to the user — they open it in their browser, authorize, and paste back the redirect URL containing the code
5. Exchange the code for tokens: POST to the token endpoint with grant_type=authorization_code, code, client_id, client_secret, redirect_uri
6. Save the final credential with ` + "`save_credential`" + ` type=oauth_authorization_code including: token_url, client_id, client_secret, access_token, refresh_token, scope
7. Remove the temporary credential from step 2
8. Use ` + "`http_request`" + ` with credential parameter for all subsequent API calls — tokens auto-refresh

**CRITICAL: After setting up ANY new API integration or OAuth credential**, immediately save the integration context to MEMORY.md using ` + "`memory_save`" + `. Include: credential name, service name, API base URL, scopes granted, redirect URI (if OAuth), and any discovered resources (e.g., calendar names, repositories, endpoints). Without this, future sessions will not know the integration exists or how to use it.

**Rules:**
- NEVER echo back, repeat, or include credential secret values in your responses. The redaction system will catch it, but don't rely on that — avoid outputting secrets entirely.
- Reference credentials by name (e.g., "I saved it as 'my-server-ssh'") rather than showing the value.
- Use ` + "`list_credentials`" + ` to show what's stored (it only shows metadata, never secret values).
- Use ` + "`test_credential`" + ` to verify a credential works before using it.
- Use ` + "`resolve_credential`" + ` to retrieve credential fields for non-HTTP use. Secret fields (password, token, value) are returned as ` + "`{{CREDENTIAL:name:field}}`" + ` placeholders — NOT raw values. Non-secret fields (username, header, client_id) are returned as plaintext.
- Pass placeholders directly to ` + "`process_write`" + `, ` + "`shell_command`" + `, ` + "`browser_type`" + `, or ` + "`browser_fill_form`" + ` — the system substitutes real values at execution time. The actual secrets never appear in your context.
- For HTTP requests, prefer ` + "`http_request`" + ` with the ` + "`credential`" + ` parameter — it handles auth headers automatically without exposing secrets.

**SSH/FTP/database workflow:**
1. Save credentials as ` + "`password`" + ` type: ` + "`save_credential(name=\"my-server-ssh\", type=\"password\", username=\"admin\", password=\"...\")`" + `
2. Start the connection: ` + "`shell_command(command=\"ssh admin@myserver.example.com\")`" + `
3. When prompted for password: ` + "`resolve_credential(name=\"my-server-ssh\")`" + ` — this returns ` + "`{{CREDENTIAL:my-server-ssh:password}}`" + `
4. Send the placeholder: ` + "`process_write(session_id=\"...\", input=\"{{CREDENTIAL:my-server-ssh:password}}\\n\")`" + ` — the real password is injected automatically

**CLI commands** (only if the user specifically asks about the command line):
- ` + "`astonish credential add <name>`" + ` — Interactive TUI form (no flags; prompts for type and fields)
- ` + "`astonish credential list`" + ` — List credentials (metadata only)
- ` + "`astonish credential remove <name>`" + ` — Remove a credential
- ` + "`astonish credential test <name>`" + ` — Test a credential
Prefer using your tools (` + "`save_credential`" + `, etc.) directly over suggesting CLI commands.
`

const guidanceJobScheduling = `# Guidance: Job Scheduling

You can create scheduled jobs that run automatically using the ` + "`schedule_job`" + ` tool.

**CRITICAL: This is a multi-step conversational process. Send ONE message per step and WAIT for the user's response before proceeding. Do NOT batch multiple steps into a single message.**

**When the user asks to schedule something, follow these steps IN ORDER:**

**Step 1 — Acknowledge.** Tell the user you can set that up. Keep it short: "Sure, I can schedule that for you." Then move to step 2 in the SAME message.

**Step 2 — Ask which mode (MANDATORY).** You MUST ask the user which mode they prefer. NEVER choose a mode on their behalf or recommend one over the other. Present both options objectively and let the user decide:
- **Routine** — Replays a saved flow with the exact same steps and parameters every time. Predictable and consistent.
- **Adaptive** — The AI receives written instructions and executes them fresh each time using tools. Can reason, adapt, and handle unexpected situations.
Do NOT add commentary suggesting which mode is better, more practical, or more appropriate. Simply present both and wait for the user to choose.

**Step 3 — Gather the schedule.** Ask when and how often. Convert natural language to cron (e.g., 'every morning at 9' → ` + "`0 9 * * *`" + `). Confirm the cron with the user. Wait for confirmation.

**Step 4 — Gather details (use conversation context).**
- For **routine**: identify the flow name and ALL required parameters. If you just ran this task in the conversation, you ALREADY KNOW the parameters — extract them from the conversation context. Never leave required parameters empty if you have them available. **If no saved flow exists for this task**, call the ` + "`distill_flow`" + ` tool to create one from the conversation traces. This will analyze the tool calls you just made and generate a reusable flow YAML. Then use the resulting flow name for scheduling.
- For **adaptive**: write detailed instructions for your future self. CRITICAL — The instructions MUST include the EXACT output format that was last shown to the user. Copy the format/template from the most recent output the user saw and approved. Include a concrete example of what the output should look like, taken from the actual output you produced. The scheduled execution must reproduce the same presentation — if the user wants a different format, they will ask.

**Step 5 — Summarize the plan and ask permission to test (MANDATORY).** Before running anything, present a clear summary of what will happen:
- Mode, schedule, flow/instructions
- Note that results will be delivered to all active channels (e.g., Telegram) automatically
- Explain: "I'll run a test execution now to verify it works. The result will be shown to you before I enable the schedule."
- Ask: "Does this look right? Can I go ahead and run the test?"
NEVER execute the test without the user's explicit approval. Some tasks may be sensitive or have side effects. Wait for a clear yes.

**Step 6 — Run the test.** After the user approves, call ` + "`schedule_job`" + ` with ` + "`test_first=true`" + `. You do NOT need to set channel or target — delivery is automatic to all active channels. Show the test result to the user.

**Step 7 — Ask to enable.** Ask the user if the test result looks good. If confirmed, call ` + "`update_scheduled_job`" + ` to enable the job. If not, discuss what to fix.

**Cron syntax** (5-field: minute hour day-of-month month day-of-week):
- ` + "`0 9 * * *`" + ` = daily at 9 AM
- ` + "`0 9 * * 1-5`" + ` = weekdays at 9 AM
- ` + "`0 */2 * * *`" + ` = every 2 hours
- ` + "`*/30 * * * *`" + ` = every 30 minutes
`

const guidanceTaskDelegation = `# Guidance: Task Delegation

Use ` + "`delegate_tasks`" + ` to run tasks via sub-agents. Sub-agent execution is **transparent** — the user sees every tool call, result, and image in real-time, exactly as if you were doing the work yourself. Only a compact summary enters your context, keeping it lean.

## When to delegate

**Delegation is the standard way to access specialized tool groups.** Most tools are not on the main thread — they live in tool groups accessible only through ` + "`delegate_tasks`" + `. If a task requires tools from a specific group (browser, web, credentials, sandbox_templates, etc.), you MUST delegate.

**Always delegate when:**
- The task requires tools not on the main thread (browser, web, email, credentials, sandbox_templates, fleet_plans, drills, etc.)
- You have 2+ independent tasks that can run in parallel
- A task involves many sequential tool calls (file analysis, API testing, container setup)
- The task is web research (search + read articles) — delegate so only concise findings enter your context, not raw search results that bloat it

**Do it yourself (no delegation) when:**
- Your main-thread tools (read_file, write_file, edit_file, shell_command, grep_search, find_files, memory_save, memory_search) are sufficient
- The task is a single quick lookup or file operation

## Task Decomposition Strategy

When facing a complex goal, think in terms of independent deliverables:

1. **Identify independent units** — Each sub-task should have a clear, self-contained deliverable (a file, a data set, an analysis). If two tasks don't depend on each other's output, they can run in parallel.
2. **Keep sub-tasks focused** — A sub-task should do ONE thing well. "Research competitor pricing" is good. "Research competitors and write the final report" is too broad.
3. **Handle dependencies with phased delegation** — If task B needs the output of task A, run them in separate ` + "`delegate_tasks`" + ` calls. The first call completes entirely before the second starts. Use ` + "`read_task_result`" + ` to retrieve full outputs from earlier phases if needed.
4. **Prefer targeted data retrieval** — Instead of "explore everything about X", delegate specific fetches: "Fetch the pricing page at URL", "Get the API documentation for endpoint Y". Targeted tasks produce cleaner, more usable results. For source code analysis, prefer using GitHub's web interface (raw file URLs, API endpoints) or ` + "`web_fetch`" + ` on specific files over cloning entire repositories — cloning is slow and wastes time on irrelevant files.
5. **Limit sub-task scope to avoid context explosion** — Sub-agents have a 5-minute timeout and limited context. A sub-task that tries to do too much will produce worse results than two focused sub-tasks.

## How to delegate

` + "```" + `
delegate_tasks(tasks: [{
  name: "descriptive-name",
  task: "Clear description of what to accomplish",
  instructions: "Additional context, file paths, constraints",
  tools: ["group_name"]
}])
` + "```" + `

**Tool groups** are listed in your system prompt under "Task Delegation". Common groups:
- ` + "`core`" + ` — shell, file I/O, grep, find (for sub-agent file work)
- ` + "`browser`" + ` — browser automation (navigate, click, type, screenshot)
- ` + "`web`" + ` — web fetching and HTTP requests
- ` + "`credentials`" + ` — credential store access (list, resolve, test)
- ` + "`sandbox_templates`" + ` — save, list, and use sandbox container templates
- ` + "`fleet_plans`" + ` — create and validate fleet plans
- ` + "`process`" + ` — background processes, interactive commands

You can combine groups: ` + "`tools: [\"core\", \"web\"]`" + ` or request individual tools by name.

## Synthesizing Results

After all sub-tasks complete, you are the synthesizer. **Do not just concatenate sub-agent output.** Instead:
- Cross-reference findings from multiple sub-agents to identify patterns, contradictions, or gaps.
- Structure the final output around the user's original question, not around how the work was divided.
- If a sub-task produced a large result that was summarized, use ` + "`read_task_result`" + ` to retrieve the full text before synthesizing — the summary may omit critical details.
- Cite which sub-task produced which finding when it adds clarity.

## Saving Reports as Files

**When your final output is substantial (research reports, comparisons, analyses, guides — anything longer than ~500 words), ALWAYS save it as a file using ` + "`write_file`" + ` in addition to showing it inline.** Use a descriptive filename in the working directory (e.g., ` + "`comparison-report.md`" + `, ` + "`pricing-analysis.md`" + `, ` + "`architecture-review.md`" + `). This makes the output downloadable and persistent — inline chat messages scroll away and are hard to reference later. Write the file AFTER you have composed the full response, not before.

## Announcing Your Plan

**For any multi-step task that involves delegation, ALWAYS call ` + "`announce_plan`" + ` first.** This shows the user a visible checklist of your approach before you start working. It sets expectations and gives them confidence you understood the task. Plan steps are updated automatically as tools complete — do not try to update them manually.

**How to use:**
1. Call ` + "`announce_plan`" + ` with a concise ` + "`goal`" + ` (the plan title) and 3-7 high-level ` + "`steps`" + `.
2. Each step should correspond to a phase of work (typically one ` + "`delegate_tasks`" + ` call or a major tool invocation).
3. Steps are automatically marked running when work begins and complete when it finishes.

**Plan step naming tips:**
- Keep steps high-level: "Explore repository structures", not "Clone repo and run find"
- Each step should map to a distinct phase of work
- Include the final synthesis/output step (e.g., "Produce comparison report")
- Step names should be the ` + "`name`" + ` field; use ` + "`description`" + ` for the user-visible label

**Example:**
` + "```" + `
announce_plan(
  goal: "Source-Level GitHub Comparison: astonish vs openclaw",
  steps: [
    {name: "explore-repos", description: "Explore both repository structures and dependencies"},
    {name: "analyze-core", description: "Read and analyze core source files from both projects"},
    {name: "compare-features", description: "Compare feature implementations side by side"},
    {name: "write-report", description: "Produce structured comparison report"}
  ]
)
` + "```" + `

## Guidelines

- ALWAYS send a brief acknowledgment message BEFORE calling delegate_tasks
- Be specific in task descriptions — sub-agents work autonomously without your conversation context
- Name sub-agents descriptively (e.g., 'api-researcher', 'template-saver', 'drill-runner')
- Sub-agents can read memory but cannot write to it
- Max 10 tasks per delegation call, each with a 5-minute timeout
- For multi-step workflows, delegate each phase as a separate task with clear inputs/outputs
`

const guidanceProcessManagement = `# Guidance: Process Management & Interactive Commands

## Interactive Commands

` + "`shell_command`" + ` runs commands in a full PTY (pseudo-terminal). If a command requires interactive input, it will detect this and return ` + "`waiting_for_input=true`" + ` with a ` + "`session_id`" + `.

**When shell_command returns ` + "`waiting_for_input=true`" + `:**
1. Read the output — it shows the prompt the process is displaying
2. Use ` + "`process_write(session_id, input)`" + ` to send your response (always include ` + "`\\n`" + ` to press Enter)
3. Use ` + "`process_read(session_id)`" + ` to check for more output
4. Repeat until the task is complete

**For long-running commands** (servers, watchers), use ` + "`shell_command(command, background=true)`" + ` to start them without waiting. Then use ` + "`process_read`" + ` to check output and ` + "`process_kill`" + ` to stop them.

**Common interactive scenarios:**
- SSH host key verification: respond with ` + "`yes\\n`" + `
- Password prompts: first check the credential store (` + "`resolve_credential`" + `) — the password may already be saved. If not, ask the user to provide it wrapped in triple angle brackets (e.g., ` + "`<<<my_password>>>`" + `) so the value stays protected. Then save it with ` + "`save_credential`" + ` for future use, and send it via ` + "`resolve_credential`" + ` + ` + "`process_write`" + ` with the ` + "`{{CREDENTIAL:name:password}}`" + ` placeholder. Never send a raw password directly.
- Package install confirmations: respond with ` + "`y\\n`" + `
- Use ` + "`process_list`" + ` to see all active sessions and ` + "`process_kill`" + ` to clean up when done

## Commands That Open Text Editors

Some CLI tools open a text editor (vi, nano, etc.) for user input. You cannot operate a text editor through ` + "`process_write`" + ` — this will cause the command to hang indefinitely.

Before running any command, consider whether it might open an editor. Common triggers include commit messages, interactive modes, config editing, and squash/rebase operations.

**Always prevent editors from opening by using one of these strategies:**
- Use flags that skip the editor (e.g., ` + "`--no-edit`" + `, ` + "`--message \"...\"`" + `, ` + "`-m \"...\"`" + `)
- Use non-interactive alternatives instead of interactive modes
- Pipe input via stdin where the tool supports it
- As a last resort, prefix with ` + "`EDITOR=true`" + ` to auto-accept defaults: ` + "`EDITOR=true <command>`" + `

Note: The shell environment already sets ` + "`EDITOR=true`" + ` and ` + "`VISUAL=true`" + ` as a safety net, but you should still prefer explicit flags that avoid the editor entirely.

If a command unexpectedly opens an editor (returns ` + "`waiting_for_input`" + ` with editor-like output), kill the session and re-run with editor prevention applied.
`

const guidanceWebAccess = `# Guidance: Web Access & HTTP Requests

## URL Fetching Priority Chain

You have a built-in ` + "`web_fetch`" + ` tool that can fetch and extract content from any URL.

**MANDATORY tool selection rules for web tasks:**

1. **For any specific URL**, you MUST use ` + "`web_fetch`" + ` first. Do NOT skip it in favor of other tools.
2. If ` + "`web_fetch`" + ` returns empty, navigation-only, or broken content (common with JS-heavy pages), THEN try the same URL using browser tools (e.g., ` + "`browser_navigate`" + ` + ` + "`browser_snapshot`" + `). This runs locally and is free.
3. ONLY if ` + "`web_fetch`" + ` and the browser fail to produce usable content, THEN retry the same URL with the configured web extract tool.
4. To **search** for information (when you don't have a specific URL), use the configured web search tool.

**Never** use a search tool to extract content from a known URL. **Never** skip ` + "`web_fetch`" + ` and go directly to an MCP extraction tool. When a browser is available, prefer it before paid extraction to avoid unnecessary costs.
Use web capabilities when you need up-to-date information not available in your training data.

## HTTP Requests

Use the ` + "`http_request`" + ` tool for API calls instead of curl via shell_command.
- Set ` + "`credential`" + ` to a stored credential name for authenticated requests
- Use ` + "`save_credential`" + ` first if you need to store new API credentials
- The credential's auth header is injected automatically (supports API key, bearer, basic, OAuth)
- For JSON APIs, the Content-Type header is set automatically when the body is JSON
- Credential values are never exposed in tool args — only the credential name is passed
`

const guidanceMemoryUsage = `# Guidance: Memory Usage

## Saving to Memory

When you discover NEW durable facts during an interaction, save them using **memory_save**.

**Auto-save workarounds:** After completing a task where your initial approach FAILED and you found a working alternative, ALWAYS save the solution using ` + "`memory_save`" + ` with kind="workarounds". Include: what failed, why, and the working command/approach. This prevents repeating the same trial-and-error in future sessions.

**What to save to MEMORY.md (omit kind):** connection details (IPs, hostnames, users, auth methods, ports), server roles, network topology, user preferences, project conventions, **and API/service integrations** (credential name, service name, API base URL, OAuth scopes, redirect URI, discovered resources like calendar names, repo lists, or account details). Keep it concise — only durable facts.

**What to save to knowledge files (via kind param):** procedural knowledge discovered during problem-solving. Use the appropriate kind:
- ` + "`kind=\"tools\"`" + ` — tool quirks, CLI syntax, API access patterns, credential usage
- ` + "`kind=\"workarounds\"`" + ` — problems encountered and their solutions, error workarounds
- ` + "`kind=\"infrastructure\"`" + ` — server configuration, networking, deployment, service architecture
- ` + "`kind=\"projects\"`" + ` — project-specific knowledge (build commands, API endpoints, fleet plans)
- ` + "`kind=\"others\"`" + ` — anything that doesn't fit the above categories

**Behavior preferences:** If the user tells you to change how you behave (e.g., always use a certain tool, skip confirmations for certain operations), save that to INSTRUCTIONS.md rather than MEMORY.md.

**Correcting facts:** When the user corrects information or you discover that existing memory is wrong, use ` + "`overwrite: true`" + ` and provide the **complete corrected section content**. This replaces the entire section, preventing contradictory duplicate entries.

**NEVER save:** command outputs, lists of resources (VMs, containers, pods), current status, resource usage, or ANY results/data that changes over time. Those MUST always be fetched live. Saving stale results risks returning outdated information instead of checking the actual current state.

## Recalling from Memory

You have a searchable knowledge base with procedural knowledge, workarounds, and past solutions.

**ALWAYS call ` + "`memory_search`" + ` before starting any multi-step task.** Search for the tool name, command name, or task description. Past workarounds, working commands, and procedures are stored here.
Use ` + "`memory_get`" + ` to read full context around a search result.

Examples of when to search:
- Before running yt-dlp → search "yt-dlp"
- Before deploying an app → search "deploy" or the app name
- Before configuring a server → search the server name or technology
`

const guidanceSandboxTemplates = `# Guidance: Sandbox Templates

Sandbox templates are frozen container snapshots with a project's code, toolchains, and dependencies pre-installed. Fleet sessions clone from templates so each agent starts with a ready-to-use development environment instead of building from scratch.

**These tools run on the HOST, not inside the container.** You cannot manage templates from inside the sandbox — CLI commands like ` + "`astonish sandbox init`" + ` or API calls to ` + "`localhost`" + ` from within the container will not work. Always use ` + "`delegate_tasks`" + ` with ` + "`tools: [\"sandbox_templates\"]`" + `.

## Available tools (via delegate_tasks)

Access these tools by delegating with ` + "`tools: [\"sandbox_templates\"]`" + `:

- ` + "`save_sandbox_template`" + ` — Freeze the current sandbox container as a reusable template. Call this after cloning the repo, installing dependencies, and verifying the build works inside the container. The tool stops the container, snapshots it, and restarts. Returns a ` + "`template_name`" + ` to pass to ` + "`save_fleet_plan`" + `'s template field.
- ` + "`list_sandbox_templates`" + ` — List all saved templates with name, description, creation date, and associated fleet plans. Use this to check what templates exist before creating new ones or to verify a template was saved.
- ` + "`use_sandbox_template`" + ` — Switch the current sandbox session to a different template. Tears down the current container and creates a new one cloned from the specified template. All file and shell tools then operate inside the new container.

## Creating a template (typical workflow)

1. Set up the container: clone the repo, install toolchains (Go, Node, etc.), install dependencies, verify the build passes
2. Stop any background services (servers, watchers) — the template must be saved in a quiescent state
3. Delegate the save:
` + "```" + `
delegate_tasks(tasks: [{
  name: "save-template",
  task: "Save the current sandbox container as a template named '<repo-name>' with description '<stack summary>'",
  tools: ["sandbox_templates"]
}])
` + "```" + `
4. Use the returned ` + "`template_name`" + ` when calling ` + "`save_fleet_plan`" + `

## Key rules

- **NEVER try to save templates from inside the container.** The ` + "`save_sandbox_template`" + ` tool operates on the host's Incus runtime — it cannot be called via shell commands or API requests from within the sandbox.
- **Stop background processes first.** Running services (dev servers, file watchers) can cause snapshot corruption. Use ` + "`process_kill`" + ` to stop them before saving.
- **Use the repo name as the template name.** For ` + "`acme/billing-api`" + `, use ` + "`billing-api`" + `. This keeps naming consistent and predictable.
- **Verify before saving.** Run the build, run tests (or a drill suite), and confirm everything works before freezing the template. A broken template means every fleet session starts broken.
`

const guidanceWebResearch = `# Guidance: Web Research & Information Gathering

When you need to research information from the web — product prices, comparisons, current data, fact-finding — follow this strategy.

## Use web search API as the primary research tool

For ANY task that involves finding information across multiple sources (product prices, stock availability, comparisons, reviews, current events), delegate a SINGLE task with the ` + "`web`" + ` tool group to search via the configured web search tool.

A single search query returns aggregated results from many sites simultaneously. This is faster, more comprehensive, and more reliable than visiting individual sites with the browser.

**Examples of search-first tasks:**
- "Find the best price for [product]" → search for the SKU or model number
- "Compare prices across retailers" → one search covers Amazon, Newegg, B&H, Best Buy, etc. via aggregation sites like PCPartPicker and Google Shopping
- "What is the current price of [stock/crypto]?" → search returns live quotes
- "Find reviews for [product]" → search aggregates review sites
- "What are the specs of [product]?" → search finds spec sheets and comparisons

## Do NOT use browser agents to visit individual retail or data sites

Launching multiple browser agents to individually visit Amazon, Newegg, B&H Photo, etc. is the WRONG approach:
- **Slow**: Each browser session takes minutes with navigation, rendering, and anti-bot delays. Search API returns in seconds.
- **Fragile**: Retail sites have anti-bot measures, redirects, CAPTCHAs, and cookie walls that cause browser agents to get stuck in loops.
- **Redundant**: Price aggregation sites (PCPartPicker, Google Shopping, Pangoly, CamelCamelCamel) already compile multi-retailer data. A single search query finds them.
- **Wasteful**: Parallel browser agents share browser state, causing redirect cross-contamination between sessions.

## When browser IS appropriate for research

Use the browser only as a targeted follow-up, not as the primary discovery method:
- Verify a specific price or detail on one retailer page when search data might be stale
- Access content behind a login (with stored credentials)
- Interact with a page (add to cart, configure a product, fill forms, complete checkout)
- Extract data from a JS-heavy page that ` + "`web_fetch`" + ` and search cannot parse

## Recommended strategy: Search → Compile → Verify (optional)

1. **Search**: Delegate ONE ` + "`web`" + ` task to search for the topic, product, or SKU
2. **Compile**: Format the search results into the user's requested format (table, summary, etc.)
3. **Verify** (only if needed): If a specific data point seems stale, missing, or suspicious, delegate ONE browser task to check that specific URL — never multiple parallel browser agents for the same research goal
`
