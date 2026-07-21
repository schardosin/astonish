# Network Policy

Astonish sandboxes deny all outbound network traffic by default. The agent cannot reach any external host unless explicitly permitted. This zero-trust approach prevents data exfiltration, supply-chain attacks, and unintended API calls from compromised or hallucinating agents.

Three mechanisms control which endpoints a sandbox can reach:

1. **Config-level presets** — static rules baked into the sandbox at creation time (deployment config)
2. **Admin-managed rules** — dynamic allow/deny rules managed through Studio settings (platform, org, and team tiers)
3. **In-chat approval** — ephemeral, session-only approval for endpoints not covered by any rule

## How It Works

Every sandbox runs behind an L7 proxy that intercepts all outbound connections. When the agent executes a command that attempts to reach a host:

- If the host is in the allow list → connection proceeds silently
- If the host is in the deny list → connection is blocked silently (agent sees a generic network error)
- If the host is unknown → the connection is blocked and a user-facing approval dialog appears in the chat

The proxy operates at the CONNECT level for HTTPS traffic, meaning it sees the target hostname and port but not the encrypted payload.

## Config-Level Presets

The deployment configuration defines static network presets that apply to all sandboxes. These are baked into the sandbox policy at creation time.

Available presets:

| Preset | Allows |
|--------|--------|
| `code_hosting` | GitHub, GitLab, Bitbucket |
| `package_registries` | npm, PyPI, crates.io, RubyGems, Maven |
| `llm_apis` | OpenAI, Anthropic, Google AI |
| `tools` | Common developer tool APIs |
| `search` | Search engines, StackOverflow, Wikipedia |
| `cdn` | Cloudflare, CloudFront, Fastly, Akamai |

By default, all presets are enabled. You can restrict to specific presets and add custom endpoints:

```yaml
sandbox:
  openshell:
    network_policy:
      presets:
        - code_hosting
        - package_registries
      extra_endpoints:
        - host: "internal-api.company.com"
          port: 443
        - host: "*.internal.company.com"
          port: 443
```

For full deployment configuration details, see [OpenShell Deployment](../deployment/openshell.md#network-policy-presets).

## Admin-Managed Network Policy Rules

Beyond static config, administrators can manage network access rules through the Studio settings UI. These rules are stored in the database and organized in three tiers:

| Tier | Managed by | Scope |
|------|-----------|-------|
| **Platform** | Superadmins | Applies to all organizations and teams |
| **Organization** | Org admins | Applies to all teams in the org |
| **Team** | Team admins | Applies to that team only |

### Merge Semantics: Deny Wins From Above

When rules exist at multiple tiers, the merge logic is:

1. If **any** higher tier (platform or org) has a **deny** rule matching the host → the endpoint is **denied**, regardless of lower-tier allow rules
2. If **any** rule at any tier matches and is **allow** (with no higher-tier deny) → the endpoint is **allowed**
3. If no rule matches at any tier → the endpoint is **unknown** (triggers in-chat approval)

This means:
- A platform deny **cannot** be overridden by an org or team allow
- An org deny **cannot** be overridden by a team allow
- A team allow is effective only if no higher tier denies it

### Managing Rules in Studio

Network policy rules are managed from the Settings panel:

- **Team Network** — accessible to team admins; shows inherited platform and org rules (read-only) above editable team rules
- **Org Network** — accessible to org admins; shows inherited platform rules (read-only) above editable org rules
- **Platform Network** — accessible to superadmins; editable platform-wide rules

Each rule specifies:
- **Host** — the hostname or wildcard pattern
- **Port** — specific port number, or empty for any port (defaults to 443)
- **Action** — `allow` or `deny`

### How Rules Are Applied

When a chat message is processed, all allow-list rules from the effective policy (platform + org + team) are pushed to the sandbox's live proxy configuration. This means:

- Rules added after sandbox creation take effect on the next message
- No fail-then-retry cycle is needed for configured endpoints
- The agent can reach allowed hosts immediately without any visible delay or error

The same admin-managed allow/deny rules also gate **Apps** `http:` data sources. The Apps HTTP proxy runs in the Studio backend (not inside an OpenShell sandbox). By default it blocks destinations that resolve to private/internal IPs (SSRF protection). Soft-private ranges (RFC1918, CGNAT, ULA, etc.) are allowed when either:

1. An **Allow** rule exists in Studio Network Policy (platform / org / team), or
2. The host:port is listed under OpenShell config `sandbox.openshell.network_policy.extra_endpoints` (same list used for sandbox PreSeed)

**Deny** blocks the request even for public hosts. Loopback, link-local, and cloud-metadata addresses stay blocked even when allowlisted. Policy tiers load fail-soft (one store error does not wipe other tiers). In-chat network grants do **not** apply to Apps — only persistent admin rules and config `extra_endpoints`.

## In-Chat Network Approval

For endpoints that are not covered by any policy rule (neither allow nor deny), Astonish provides an interactive approval flow directly in the chat. This flow applies to **sandbox** egress only (agent tools); Apps have no interactive grant UI.

### How It Works

1. The agent runs a command that attempts to connect to an unknown host
2. The sandbox proxy blocks the connection
3. Astonish detects the blocked connection from the command output
4. The agent's execution is paused
5. An approval dialog appears inline in the chat, showing:
   - The blocked host and port
   - A suggested broader wildcard pattern (e.g., `*.example.com` for `api.example.com`)
6. The user can:
   - **Approve** the exact host
   - **Approve** the broader pattern
   - **Deny** the request
7. On approval, the endpoint is added to the sandbox's live policy and the agent automatically retries the blocked command

### Ephemeral Approvals

In-chat approvals are **session-only** — they are not persisted to the network policy settings. When the session ends (or the sandbox is recycled), the approval is lost.

To permanently allow an endpoint, add it as an admin-managed rule in the Network Policy settings.

### Decision Summary

| Policy Decision | What Happens |
|----------------|--------------|
| **Allow** (in policy) | Connection succeeds silently — no dialog, no delay |
| **Deny** (in policy) | Connection blocked silently — no dialog shown, agent sees a generic error |
| **Unknown** (not in policy) | Approval dialog shown, agent paused until user responds |

## Deny Rules

Use deny rules to block access to specific endpoints across your organization:

- **Platform deny** — blocks the endpoint for all orgs and teams; cannot be overridden
- **Org deny** — blocks the endpoint for all teams in the org; cannot be overridden by team allow
- **Team deny** — blocks the endpoint for that team only

Common use cases for deny rules:
- Block internal services that should never be accessed by the agent
- Prevent access to competitor APIs or unauthorized data sources
- Enforce compliance policies (e.g., no connections to specific regions)

## Wildcard Patterns

Host patterns support two wildcard types:

| Pattern | Meaning | Example Match | Non-Match |
|---------|---------|---------------|-----------|
| `*.example.com` | One subdomain level | `api.example.com` | `a.b.example.com` |
| `**.example.com` | Any subdomain depth | `a.b.c.example.com` | `example.com` itself |

All host matching is case-insensitive.

Examples:

```
*.github.com        → matches api.github.com, raw.github.com
**.googleapis.com   → matches storage.googleapis.com, oauth2.accounts.googleapis.com
internal.corp.com   → matches only internal.corp.com (exact)
```

## See Also

- [Sandboxes](./sandboxes.md) — sandbox isolation architecture overview
- [OpenShell Deployment](../deployment/openshell.md) — full deployment guide with config-level network presets
- [Settings](../studio/settings.md) — Studio settings panel overview
