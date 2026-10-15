# External Channels

## Overview

Channels provide bidirectional communication between the AI agent and external platforms. Users can interact with Astonish through Telegram, email, or the Studio web UI, with each channel maintaining persistent per-conversation sessions. The channel system supports slash commands, fleet integration, real-time streaming, and credential redaction on all outbound messages.

## Key Design Decisions

### Why a Plugin-Based Architecture

The `Channel` interface abstracts platform differences behind a common contract: `Start()`, `Stop()`, `Send()`, `SendTyping()`. This allows new platforms to be added without modifying the core orchestration logic. Each adapter handles platform-specific concerns (message formatting, polling, API limits) internally.

### Why Per-Conversation Persistent Sessions

Each unique conversation (identified by `<channelID>:<chatType>:<chatID>`) maps to a persistent session in the FileStore. This means:

- A Telegram user can close the app, come back days later, and continue the same conversation with full context.
- Multiple users on the same platform each get their own session.
- Fleet sessions can run across channels with their own conversation threads.

### Why Allowlist-Based Access Control

Both Telegram and email adapters use explicit allowlists. Empty allowlist = block all. This is a deliberate security choice -- the agent has access to tools, credentials, and potentially sensitive systems. Open access would be dangerous. The `/authorize` slash command allows admins to add new users to the allowlist at runtime.

### Why HTML for Telegram

Telegram supports both Markdown and HTML formatting, but its Markdown parser is strict and incompatible with standard Markdown (e.g., unmatched `_` in code causes failures). HTML is more forgiving and handles the edge cases that LLM-generated text frequently produces. The `markdownToTelegramHTML` converter handles code blocks (with placeholder substitution to prevent nested parsing), bold, italic, headings, tables (converted to bullet lists since Telegram doesn't support tables), and list items.

### Why Typing Indicators

Astonish sends typing indicators on a 4-second interval loop while the agent is processing. This was tuned for Telegram's 5-second typing indicator expiry. The user sees continuous "typing..." feedback during long operations, which is important because agent turns can take 30+ seconds with multiple tool calls.

## Account linking security

The authenticated user's Connected Channels form preselects the external account before requesting a code: Telegram numeric user ID, or Slack workspace ID (`T…`) and member ID (`U…`/`W…`). `channels.LinkIdentity` validates and canonicalizes principals; `LinkBinding` stores `link:v1:telegram:<id>` or `link:v1:slack:<workspace>:<member>` in the existing pending channel column. Legacy unbound Telegram/Slack codes fail closed and must be regenerated. No identity is selected by the first presenter. Slack adapters must use the same workspace-qualified principal for linking, allowlists, inbound routing and delivery resolution; bare member IDs are not interchangeable.

`/link` remains reachable before allowlist checks, only from a validated real user's direct message. Adapters retain the callback signature `(ctx, senderID, senderName, code)`; Slack passes `T…:U…` derived from authenticated event/workspace data, never command text. Shared API `ConsumeChannelLinkCode` rate-limits before lookup, validates channel and preselected principal before consume, then the daemon persists the verified binding. Wrong sender/workspace/channel leaves the token usable by its owner. Immutable random records and storage's atomic single-winner delete prevent replay; a persistence failure after consume requires a new code.

Verification uses 128-bit random, case-sensitive 22-character URL-safe tokens. Email retains authenticated ownership and email-channel checks and the UI neither uppercases nor truncates pasted codes.

**Operational limitation:** attempt budgets currently reside in process memory: 10 attempts per channel/principal and 1,000 per channel per five-minute window, at most 4,096 tracked principal hashes. All attempts count; saturation fails closed. Email is keyed by authenticated Astonish user ID. Budgets reset on restart and multiply across replicas; this is not distributed brute-force protection. Before scaling verification workers, enforce shared channel/principal budgets in a gateway or distributed limiter. Pending bindings themselves are persisted through the platform DB backend, with no tenant-scope changes. Real Telegram/Slack service integration and distributed throttling require deployment validation.

### Slack workspace compatibility and migration

- Platform `user_channels.external_id` and inbound `SenderID` are canonical `T…:U…` (or `T…:W…`). Inbound `ChatID` is `T…:D…`/`T…:C…`/`T…:G…`, preserving workspace isolation in sessions, slash commands, mentions, fleet routing and replies. Slack `Send` selects only the matching registered client or the default client whose `auth.test` team matches; an unknown workspace is an error, never a default-client fallback.
- Scheduled linked-user delivery and broadcasts use qualified member targets. `conversations.open` receives the **bare member ID** using that workspace's token; text and attachments then use the returned bare conversation ID. Qualified conversation replies are stripped only after workspace client selection.
- Personal-mode configured bare allowlist entries remain compatible **only within the default workspace authenticated by `auth.test`**. They never authorize a sender from another workspace, and a bare outbound target without an authenticated default workspace fails. OAuth-only deployments must use qualified targets.
- Existing bare verified **platform links** cannot prove their workspace. Startup/reload allowlist refresh excludes them and logs unlink/relink guidance; inbound platform lookup rejects bare principals and never retries a qualified sender as a bare member. Delivery resolution rejects these links. There is no automatic workspace guessing or silent migration: remove the old Slack connection in Settings → Channels, select its workspace and member IDs, generate a new code, and verify it in a DM with `/link CODE`. Existing conversation sessions keyed by bare chat IDs are not automatically reassigned to qualified sessions.
- Explicit owner/member/team delivery resolution failures (including missing resolvers or legacy links) block delivery and retain the cause; they do **not** fall back to broadcasting. Multi-recipient resolution aborts before sending if any recipient cannot be resolved. Empty legacy personal delivery mode retains its explicit broadcast behavior.

## Architecture

### Message Flow

```
External Platform (Telegram/Email/Slack)
    |
    v
Channel Adapter:
  - Poll for new messages (long polling / IMAP)
  - Validate sender against allowlist
  - Normalize to InboundMessage
    |
    v
ChannelManager.handleInbound():
  Priority chain:
  1. Slash commands (/status, /new, /distill, /fleet, etc.)
  2. Active fleet sessions (route to fleet agent)
  3. Regular ChatAgent (standard conversation)
    |
    v
For regular messages:
  - Resolve or create persistent session
  - Start typing indicator loop
  - Run ADK runner with ChatAgent
  - Collect streaming text events
  - Collect images from tool results (browser screenshots)
  - Stop typing indicator
    |
    v
ChannelManager -> Channel Adapter:
  - Apply credential redaction
  - Format for platform (HTML for Telegram, mrkdwn/Block Kit for Slack, plain for email)
  - Chunk if needed (Telegram: 4096 chars, Slack: 4000-char text fallback, Email: unlimited)
  - Attach images if present
  - Send to platform
```

### Slash Commands

| Command | Description |
|---|---|
| `/status` | Show agent status and active sessions |
| `/new` | Start a new conversation (new session) |
| `/distill` | Trigger flow distillation from current session traces |
| `/jobs` | List scheduled jobs |
| `/authorize` | Add a new user to the allowlist |
| `/help` | List available commands |
| `/fleet` | List fleet plans, start a fleet session |
| `/fleet_plan` | Start fleet plan creation wizard |
| `/fleet_stop` | Stop the active fleet session |

Commands are registered in a thread-safe `CommandRegistry` and can be refreshed dynamically via the `CommandRefresher` interface (e.g., updating Telegram's bot command menu).

### Fleet Integration

When a fleet session is active in a channel conversation:

1. All user messages are routed to the fleet's `PostHumanMessage()` instead of the ChatAgent.
2. Fleet agent responses are forwarded back to the channel via the `OnMessagePosted` callback.
3. Session completion triggers automatic cleanup and notification.
4. Fleet commands (`/fleet_stop`) can control the session lifecycle.

### Telegram Adapter

- **Polling**: Long polling via `go-telegram-bot-api` with configurable timeout.
- **Message chunking**: Messages over 4096 characters are split at paragraph, line, or word boundaries.
- **Image support**: Photos are sent with optional captions (max 1024 chars).
- **Bot commands**: Registered via the Telegram Bot API's `setMyCommands` for autocomplete in the client.
- **Broadcast**: `BroadcastTargets()` returns one target per allowed user ID, enabling scheduled messages to all users.

### Email Adapter

- **IMAP polling**: Checks for new messages at configurable intervals (default 30 seconds).
- **Deduplication**: `seenIDs` map tracks processed message IDs (pruned at 10K entries).
- **Threading**: Replies use `In-Reply-To` and `References` headers to maintain email threads.
- **Allowlist**: Supports `["*"]` wildcard for allowing all senders.
- **Format**: Plain text (no HTML formatting needed).
- **Receiver trust**: `channels.email.require_sender_auth` defaults to true (omitted/nil). Set `channels.email.trusted_authserv_id` to the receiving mail system's exact DNS-style authserv-id (for example, `mx.example.com`), also stored as `trusted_authserv_id` in platform Email settings. Missing/invalid IDs fail channel startup before connection; authentication is never silently disabled. IMAP and Graph preserve repeated `From` and `Authentication-Results` in `Message.HeaderValues` occurrence order; Graph explicitly selects `internetMessageHeaders`. The flattened `Headers` map is first-copy compatibility data, not authentication evidence.
- **Acceptance boundary**: Require exactly one From field containing one mailbox. The topmost Authentication-Results field must come from the configured receiver and contain an exact parsed `dmarc=pass` with an exactly matching `header.from` domain in that same method result. Duplicate trusted receiver fields, duplicate From, split results, malformed/unsupported syntax, and older passing results do not authorize a sender. The conservative RFC 8601 subset supports common folding, nested comments and quoted property values; unsupported forms fail closed. The allowlist is checked only after authentication. Thread headers do not grant access to another sender's session.
- **Required mail-system prerequisite**: The configured receiver **must strip externally supplied Authentication-Results fields claiming its own authserv-id**, then prepend its own result. Confirm this with the mail administrator and a controlled injected-header delivery test. An authserv-id string is **not cryptographic authenticity**; configuring an ID without this sanitization is unsafe. Intermediaries that prepend another receiver's result or providers that omit raw headers fail closed and need a compatible receiving path, not an automatic downgrade.
- **Migration**: Before upgrading an existing email deployment, identify the final receiver's authserv-id from a legitimate raw message, verify its stripping/prepending behavior, and save `trusted_authserv_id` in the effective host YAML or platform settings. Test an aligned legitimate sender and a controlled forged-header message. Existing deployments lacking the setting will show an actionable startup error. For isolated local/test mail systems only, explicitly set `require_sender_auth: false`; startup logs a WARNING that spoofed From identities are permitted. This escape hatch retains allowlists but makes their sender identity unverified and is not recommended for production.

### Slack Adapter

- **Transport**: Socket Mode is the default WebSocket transport; Events API mode uses `POST /api/slack/events` for webhooks.
- **Slash commands and thread commands**: Slack slash commands are delivered separately from message events. `/link <code>` is handled through Socket Mode `slash_commands` envelopes or `POST /api/slack/commands` for HTTP mode, and the adapter returns an ephemeral Slack response immediately. Other registered Astonish commands are normalized back into `InboundMessage` and routed through `ChannelManager`, so Slack reuses the same `CommandRegistry` implementation as Telegram. Slack does not support custom app slash commands inside thread reply composers, so Astonish supports app-mention commands in threads instead: `@Astonish status`, `@Astonish /status`, and `@Astonish /astonish-status` are normalized to `/status`. Because these are normal `app_mention` events, Slack includes `thread_ts`, and command responses stay in the originating thread.
- **Command registration**: When Slack App Manifest credentials are configured (`app_id` plus an app configuration token), the adapter exports the current Slack app manifest, merges Astonish-owned commands, validates the manifest, and updates it best-effort. In Socket Mode it sets `settings.socket_mode_enabled=true` and omits slash-command URLs; in Events API mode each command uses the configured HTTPS command URL. `/link` keeps its bare name because it is part of the account-linking UX; shared registry commands use Slack-safe app-prefixed aliases such as `/astonish-status` because Slack reserves many generic slash command names. Session-scoped commands such as `/new`, `/distill`, and fleet session commands are not exposed as Slack slash commands because Slack slash-command payloads do not carry `thread_ts`; users start fresh by creating a new Slack thread. The exported manifest is preserved outside the owned command entries because Slack manifest updates are exhaustive.
- **Linking before allowlist**: `/link <code>` is accepted before the sender is in the dynamic allowlist so first-time users can connect their Slack identity to their Astonish account.
- **Conversation routing**: DMs use `message.im`; channel conversations use `app_mention`; replies stay in the originating Slack thread. Slack normal messages set `ThreadID` to the Slack `thread_ts` or the top-level message timestamp, so each Slack thread maps to a distinct Astonish session.
- **Rendering**: Agent responses arrive from `ChannelManager` as generic rich text. The Slack adapter owns Slack-specific rendering in `pkg/channels/slack/render.go`: it normalizes Markdown/HTML-like output to Slack `mrkdwn`, preserves plain text and code-heavy messages as chunked text, and selectively uses Block Kit for structured summaries.
- **Block Kit enrichment**: The renderer uses `section` blocks for titles/status groups, `section.fields` for compact key/value summaries, Slack `table` blocks for generic Markdown tables and grouped inventory-style lists, and a small `context` block for non-sensitive Astonish metadata. Every Block Kit message also includes fallback text for notifications and accessibility. If a response exceeds Slack's safe block/text limits, the adapter falls back to chunked `mrkdwn` text.

### Channel Hints

Each channel injects platform-specific hints into the agent's system prompt so the model can choose an output shape suited to that platform before adapter rendering happens:

- **Slack**: Use Slack-compatible Markdown/mrkdwn-friendly formatting. For structured records, prefer compact Markdown tables when the result set is small or medium and meaningful columns exist; for long result sets, summarize first and use grouped bullets or offer more detail. This instruction is domain-agnostic and must not mention specific tools, providers, or resource types.
- **Telegram**: Keep responses concise. Avoid tables because Telegram does not render them reliably; use bullet points instead.
- **Email**: Responses can be longer and more formal. Include subject-appropriate structure.
- **Console/Studio**: No hints (default behavior).

## Key Files

| File | Purpose |
|---|---|
| `pkg/channels/manager.go` | ChannelManager: orchestration, inbound routing, streaming, typing |
| `pkg/channels/channel.go` | Channel interface definition |
| `pkg/channels/router.go` | Session key derivation for per-conversation persistence |
| `pkg/channels/commands.go` | CommandRegistry and built-in slash commands |
| `pkg/channels/fleet_commands.go` | Fleet-specific slash commands |
| `pkg/channels/telegram/telegram.go` | Telegram adapter: polling, HTML formatting, chunking |
| `pkg/channels/email/email.go` | Email adapter: IMAP polling, SMTP sending, threading |
| `pkg/channels/slack/slack.go` | Slack adapter: Socket Mode, Events API dispatch, slash-command linking |
| `pkg/channels/slack/events.go` | Slack HTTP handlers for Events API and slash commands |
| `pkg/channels/slack/manifest.go` | Slack App Manifest command synchronization from the shared command registry |
| `pkg/channels/slack/render.go` | Slack-native rendering: mrkdwn conversion, Block Kit summaries, fallback chunking |

## Interactions

- **Agent Engine**: Regular messages go through ChatAgent with a dedicated session per conversation.
- **Sessions**: Per-conversation persistent sessions in the FileStore.
- **Credentials**: All outbound messages pass through the Redactor.
- **Fleet**: Active fleet sessions intercept messages for fleet agent routing.
- **Scheduler**: Scheduled jobs can send results to channel targets via `Send()`.
- **Daemon**: ChannelManager is initialized during daemon startup with hot-reload support for configuration changes.
