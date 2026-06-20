# Authentication

Astonish supports different authentication strategies depending on the deployment. Local deployments use lightweight device authorization; cloud deployments with PostgreSQL offer built-in JWT authentication and federated OIDC sign-in.

## Local (SQLite)

In local deployments, Astonish runs as a single-user daemon. Authentication uses a **device authorization code** flow:

```bash
astonish auth login
```

The CLI opens a browser for device verification and stores the resulting token locally. No user management is required.

## Cloud (PostgreSQL)

Platform deployments support multiple users and teams. Two authentication mechanisms are available, and both can be enabled simultaneously.

### Built-in Authentication

Built-in auth uses bcrypt-hashed passwords with JWT bearer tokens:

| Token Type    | Lifetime | Storage        | Algorithm   |
|---------------|----------|----------------|-------------|
| Access token  | 15 min   | Authorization header | HMAC-SHA256 |
| Refresh token | 90 days  | HttpOnly cookie | HMAC-SHA256 |

Access tokens are short-lived to limit blast radius. Refresh tokens are stored as `HttpOnly`, `Secure`, `SameSite=Strict` cookies — inaccessible to client-side JavaScript.

#### JWT Claims Structure

```json
{
  "sub": "user-uuid",
  "org": "org-uuid",
  "teams": ["backend", "infra"],
  "role": "member",
  "iat": 1718900000,
  "exp": 1718900900
}
```

The `teams` array drives authorization decisions across the platform — sandbox access, credential resolution, and audit filtering all reference these claims.

### OIDC Federation

For enterprises with existing identity providers, Astonish federates authentication via **Authorization Code + PKCE**. Supported providers include:

- SAP Identity Authentication Service (IAS)
- Microsoft Entra ID (Azure AD)
- Okta
- Any standards-compliant OIDC provider

#### Configuration

```yaml
# config.yaml
auth:
  oidc:
    issuer: https://accounts.example.com
    client_id: astonish-platform
    redirect_uri: https://astonish.example.com/auth/callback
    scopes:
      - openid
      - profile
      - groups
```

No `client_secret` is required — PKCE eliminates the need for server-side secrets in the authorization flow.

#### Team Auto-Mapping

When the OIDC provider includes a `groups` claim in the ID token, Astonish automatically maps those groups to internal team memberships:

```yaml
auth:
  oidc:
    team_mapping:
      claim: groups
      # Optional prefix stripping (e.g., "astonish-backend" → "backend")
      strip_prefix: "astonish-"
```

On each login, team memberships are reconciled — users are added to teams matching their group claims and removed from teams no longer present. This keeps access in sync with your identity provider without manual administration.

## Token Lifecycle

1. User authenticates (built-in or OIDC).
2. Server issues an access token (15 min) and refresh token (90 days).
3. API requests include the access token in the `Authorization: Bearer` header.
4. On expiry, the client silently exchanges the refresh token for a new access token.
5. Refresh token rotation: each use invalidates the previous refresh token.

## Configuration Reference

```yaml
auth:
  # HMAC-SHA256 signing key (required, generate with: openssl rand -hex 32)
  jwt_secret: "${JWT_SECRET}"

  # Built-in auth settings
  builtin:
    enabled: true
    password_min_length: 12

  # OIDC federation
  oidc:
    enabled: true
    issuer: https://login.example.com
    client_id: astonish
    redirect_uri: https://astonish.example.com/auth/callback
    scopes: [openid, profile, groups]
    team_mapping:
      claim: groups
```

## See Also

- [Credential Security](./credential-security.md) — how credentials are protected after authentication
- [Audit Logging](./audit-logging.md) — authentication events are recorded in the audit trail
