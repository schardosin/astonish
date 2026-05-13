# Remote CLI Client Architecture

This document describes the architecture for making the Astonish CLI a thin client to a remote Astonish platform server, enabling the same CLI experience for both personal (local) and platform (remote) deployments.

## Overview

Single binary (`astonish`), dual mode:

- **Personal mode** (default): CLI runs everything in-process or talks to a local daemon on `localhost`. No auth required. Current behavior, unchanged.
- **Remote mode** (after `astonish login`): CLI becomes an HTTP client to a remote Astonish server, using the same REST/SSE APIs that the Studio web UI uses.

When `~/.config/astonish/remote.yaml` exists and has a URL configured, all user-facing commands route through the remote HTTP client. Infrastructure commands (`daemon`, `studio`) emit a hard error.

## Design Principles

- **Same UX**: The chat experience, tool approval flow, flow execution output, and session management are identical regardless of mode.
- **Progressive complexity**: Users start personal, then `astonish login <url>` switches them to remote. `astonish logout` switches back.
- **No local daemon in remote mode**: The CLI talks directly to the remote server. Running `daemon start` or `studio` in remote mode is an error.
- **Encrypted credential storage**: JWT tokens are stored locally using the same AES-256-GCM mechanism as the credential store.
- **Auto-refresh**: Access tokens (15 min TTL) are refreshed transparently. Expired refresh tokens (90 days) prompt re-login.

## Command Classification

### User-Facing (Become Remote)

| Command | Remote Behavior |
|---------|----------------|
| `chat` | POST /api/studio/chat + SSE streaming |
| `sessions list/show/delete` | GET/DELETE /api/studio/sessions |
| `flows run` | POST /api/run + SSE streaming |
| `flows list/show` | GET /api/flows |
| `scheduler list/enable/disable/remove/run/status` | Same daemon API, remote URL + auth |
| `fleet list/show/activate/deactivate/status/delete/templates` | Same daemon API, remote URL + auth |
| `drill run/list/report` | GET /api/drills, POST /api/studio/chat |

### Infrastructure (Stay Local / Error in Remote Mode)

| Command | Remote Behavior |
|---------|----------------|
| `daemon start/stop/install/...` | Hard error: "Not available in remote mode" |
| `studio` | Hard error: "Open {url} in your browser" |
| `setup` | Local only |
| `config` | Local only |
| `sandbox` | Local only |
| `tap` | Local only |
| `tools edit/store` | Local only |
| `memory` | Local only |
| `migrate` | Local only |
| `platform` | Local only |

### New Commands (Remote Mode Only)

| Command | Purpose |
|---------|---------|
| `astonish login <url>` | Authenticate (email/password or `--sso`) |
| `astonish logout` | Clear tokens, remove remote config |
| `astonish status` | Show mode (local/remote), server URL, org, team, user |
| `astonish org use <slug>` | Switch active org |
| `astonish org list` | List available orgs |
| `astonish team use <slug>` | Switch active team |
| `astonish team list` | List available teams |

## Configuration

### Remote Config: `~/.config/astonish/remote.yaml`

```yaml
url: https://astonish.mycompany.com
org: my-org
team: general
user_email: alice@example.com
```

### Encrypted Token Storage: `~/.config/astonish/remote_tokens.enc`

Same format as `credentials.enc` — AES-256-GCM using the existing `.store_key` file.

Contents (decrypted):
```json
{
  "access_token": "eyJhbG...",
  "refresh_token": "eyJhbG...",
  "access_expires_at": "2025-01-15T10:30:00Z",
  "refresh_expires_at": "2025-04-15T10:15:00Z"
}
```

## Architecture

### Dispatch Flow

```
CLI Command
    |
    +-- isRemoteMode()? --No--> Local execution (current behavior)
    |
    +-- Yes --> pkg/client.Client
                   |
                   +-- Auth: Authorization: Bearer <access_token>
                   +-- Headers: X-Astonish-Team: <team-slug>
                   +-- Auto-refresh on 401
                   +-- Org from JWT claims (set at login time)
                   |
                   +-- REST calls (sessions, flows, scheduler, fleet)
                   +-- SSE streaming (chat, flow execution)
```

### Package Structure

```
pkg/client/
+-- config.go          # Remote config load/save (remote.yaml)
+-- tokens.go          # Encrypted token storage (AES-256-GCM)
+-- client.go          # Core HTTP client with auth, headers, auto-refresh
+-- auth.go            # Login (password + SSO), logout, refresh
+-- sse.go             # Generic SSE stream reader
+-- chat.go            # Chat-specific SSE client (event parsing)
+-- api.go             # Typed API methods (sessions, flows, drills, etc.)

cmd/astonish/
+-- login.go           # astonish login
+-- logout.go          # astonish logout
+-- remote_status.go   # astonish status
+-- org.go             # astonish org use/list
+-- team.go            # astonish team use/list
```

## Server-Side Changes Required

### 1. Bearer Token Support in Auth Middleware

Currently the platform auth middleware reads JWT exclusively from the `astonish_access` cookie. Add fallback to `Authorization: Bearer <token>` header:

```go
// Try Authorization header first (for CLI clients)
if authHeader := r.Header.Get("Authorization"); strings.HasPrefix(authHeader, "Bearer ") {
    tokenStr = strings.TrimPrefix(authHeader, "Bearer ")
} else {
    // Fall back to cookie (for browser)
    cookie, err := r.Cookie(accessCookieName)
    ...
}
```

### 2. Return Tokens in Login Response Body

The login endpoint currently only sets cookies. For CLI clients, also return tokens in the JSON body:

```json
{
  "user": { ... },
  "org": { ... },
  "access_token": "eyJhbG...",
  "refresh_token": "eyJhbG...",
  "expires_in": 900
}
```

Triggered when request includes `"client_type": "cli"` or `Accept: application/json` without browser indicators.

### 3. Refresh Token via Request Body

The `/api/auth/refresh` endpoint currently reads the refresh token from a cookie. Add support for JSON body:

```json
POST /api/auth/refresh
{"refresh_token": "eyJhbG..."}
```

Response:
```json
{
  "access_token": "eyJhbG...",
  "refresh_token": "eyJhbG...",
  "expires_in": 900
}
```

## Authentication Flows

### Password Login

```
astonish login https://astonish.company.com
> Email: alice@example.com
> Password: ********

POST https://astonish.company.com/api/auth/login
{"email": "alice@example.com", "password": "...", "client_type": "cli"}

Response: {access_token, refresh_token, user, org}

--> Store tokens in remote_tokens.enc
--> Write remote.yaml with url, org, team, user_email
--> "Logged in as alice@example.com (Acme Corp / engineering)"
```

### SSO Login

```
astonish login https://astonish.company.com --sso

1. CLI starts localhost HTTP server on random port
2. Opens browser: https://astonish.company.com/api/auth/oidc/authorize?
     redirect_uri=http://localhost:<port>/callback&
     cli=true
3. User authenticates with IdP (SAP IAS, Azure AD, Okta)
4. Server completes OIDC exchange, redirects to CLI callback with tokens:
     http://localhost:<port>/callback?access_token=...&refresh_token=...
5. CLI receives tokens, stores them, closes server
6. "Logged in as alice@example.com (Acme Corp / engineering)"
```

### Token Refresh (Automatic)

```
Any API call returns 401
--> Client intercepts
--> POST /api/auth/refresh {"refresh_token": "..."}
--> New tokens stored
--> Original request retried with new access token

If refresh also fails (expired):
--> "Session expired. Run 'astonish login' to re-authenticate."
--> Exit with error code
```

## Chat Protocol (Remote Mode)

The remote chat follows the same SSE protocol that the Studio web UI uses.

### Turn Flow

```
1. User types message
2. CLI sends: POST /api/studio/chat
   {"message": "...", "sessionId": "<id>", "autoApprove": false}
3. Server responds with SSE stream
4. CLI parses SSE events:

   event: session    --> Store session ID
   event: text       --> Stream text to terminal
   event: tool_call  --> Show tool box (name + args)
   event: tool_result --> Show result
   event: approval   --> Prompt user with huh form
   event: artifact   --> Show file notification
   event: usage      --> Display token count
   event: error      --> Show error box
   event: done       --> End of turn, show prompt

5. If approval needed:
   - Show approval prompt locally
   - User responds "Yes" / "No"
   - Send new POST with that response as the message
   - Read new SSE stream (continues execution)

6. User types next message --> goto 2
```

### Reconnection

If the CLI disconnects mid-stream (network issue):

```
1. GET /api/studio/sessions/{id}/status
   {"running": true, "eventCount": 42}

2. GET /api/studio/sessions/{id}/stream  (SSE)
   --> Replays missed events
   --> Continues with live events
```

### Resume (--resume flag)

```
1. GET /api/studio/sessions (list recent)
2. Use most recent session ID
3. Continue from there with new POST
```

## TUI Refactoring for Dual Mode

The current chat TUI in `pkg/launcher/chat_console.go` is tightly coupled to the ADK event iterator. For remote mode, the same rendering must be driven by SSE events.

### Strategy: Event Abstraction Layer

```go
// pkg/ui/events.go — Abstract chat events (mode-agnostic)
type ChatEvent struct {
    Type    ChatEventType
    Text    string          // for Text events
    Tool    string          // for ToolCall/ToolResult/Approval
    Args    map[string]any  // for ToolCall
    Result  string          // for ToolResult
    Options []string        // for Approval
    Error   string          // for Error events
    Session string          // for Session event
}

type ChatEventType int
const (
    EventSession ChatEventType = iota
    EventText
    EventToolCall
    EventToolResult
    EventApproval
    EventAutoApproved
    EventArtifact
    EventUsage
    EventError
    EventDone
)
```

### Two Backends

```go
// Local backend (current code, refactored)
func RunLocalChat(ctx context.Context, cfg ChatConfig) <-chan ChatEvent

// Remote backend (new)
func RunRemoteChat(ctx context.Context, client *client.Client, sessionID, message string) <-chan ChatEvent
```

### Shared Presenter

```go
// Drives the TUI regardless of source
func RunChatPresenter(events <-chan ChatEvent, approvalFn func(tool string, options []string) string)
```

The presenter handles:
- Starting/stopping spinners
- Streaming text to terminal
- Rendering tool boxes
- Showing approval prompts (calls `approvalFn`)
- Displaying errors
- Managing the input prompt loop

## Implementation Phases

### Phase 1: Server-Side Auth Changes
- Add Bearer token support in platform auth middleware
- Return tokens in login response body (when `client_type: "cli"`)
- Accept refresh token in request body on `/api/auth/refresh`

### Phase 2: Client Infrastructure (`pkg/client`)
- `config.go` — Remote config load/save
- `tokens.go` — Encrypted token storage
- `client.go` — HTTP client with auth headers + auto-refresh
- `auth.go` — Login (password + SSO stub), logout, refresh
- `sse.go` — Generic SSE stream reader

### Phase 3: Login, Logout, Status Commands
- `astonish login <url>` — interactive auth flow
- `astonish logout` — clear tokens + remote.yaml
- `astonish status` — show current mode/connection
- `astonish org use/list` — org switching
- `astonish team use/list` — team switching
- Guard `daemon`/`studio` commands in remote mode

### Phase 4: Remote Sessions
- `sessions list` via API
- `sessions show` via API
- `sessions delete` via API

### Phase 5: Remote Flows
- `flows list` via API
- `flows run` via SSE streaming

### Phase 6: Remote Scheduler, Fleet, Drill
- Modify `getAPIBaseURL()` to return remote URL when configured
- Add auth headers to existing HTTP calls
- `drill list/report` via API

### Phase 7: Remote Chat
- Refactor TUI into event abstraction + presenter
- Implement SSE chat client in `pkg/client/chat.go`
- Wire remote backend into `chat_console.go`
- Handle approval flow over SSE
- Handle reconnection on network failure
- `--resume` flag in remote mode

## Error Messages

```
# daemon start in remote mode
Error: 'daemon start' is not available in remote mode.
You are connected to https://astonish.company.com
Use 'astonish logout' to disconnect and return to personal mode.

# studio in remote mode
Error: 'studio' is not available in remote mode.
Open https://astonish.company.com in your browser instead.
Use 'astonish logout' to disconnect and return to personal mode.

# expired refresh token
Error: Session expired. Run 'astonish login' to re-authenticate.

# server unreachable
Error: Cannot reach https://astonish.company.com
Check your network connection or use 'astonish logout' to switch to personal mode.

# 403 on team access
Error: Access denied. You are not a member of team 'engineering'.
Use 'astonish team list' to see available teams.
```

## Security Considerations

- Tokens are encrypted at rest using AES-256-GCM (same key as credential store)
- Access tokens are short-lived (15 min) — limits exposure if leaked
- Refresh tokens are long-lived (90 days) but revocable server-side
- No tokens are ever printed to stdout or logged
- `astonish logout` cryptographically erases stored tokens (overwrites file)
- The CLI never stores the user's password — only the issued tokens
- SSO flow uses a localhost callback (no tokens in URL history after redirect)
