# Authentication

Astonish uses JWT-based authentication with support for both built-in email/password login and federated OIDC sign-in. Both mechanisms can be enabled simultaneously.

## Built-in Authentication

Built-in auth uses bcrypt-hashed passwords with JWT bearer tokens:

| Token Type    | Lifetime | Storage        | Algorithm   |
|---------------|----------|----------------|-------------|
| Access token  | 15 min   | Authorization header | HMAC-SHA256 |
| Refresh token | 90 days  | HttpOnly cookie | HMAC-SHA256 |

Access tokens are short-lived to limit blast radius. Refresh tokens are stored as `HttpOnly`, `Secure`, `SameSite=Strict` cookies — inaccessible to client-side JavaScript.

### JWT Claims Structure

```json
<
  "sub": "user-uuid",
  "org": "org-uuid",
  "teams": ["backend", "infra"],
  "role": "member",
  "iat": 1718900000,
  "exp": 1718900900
>
```

The `teams` array drives authorization decisions across the platform — sandbox access, credential resolution, and audit filtering all reference these claims.

## OIDC Federation

For enterprises with existing identity providers, Astonish federates authentication via **Authorization Code + PKCE**. Supported providers include:

- SAP Identity Authentication Service (IAS)
- Microsoft Entra ID (Azure AD)
- Okta
- Any standards-compliant OIDC provider

### Configuration

OIDC providers are configured per-organization in the platform database. Each provider stores:

| Field | Description |
|-------|-------------|
| `issuer_url` | OIDC issuer URL |
| `discovery_url` | OpenID Connect discovery endpoint |
| `client_id` | OAuth2 client ID |
| `client_secret` | OAuth2 client secret (if required) |
| `scopes` | Requested scopes (default: `openid, email, profile`) |
| `team_claim` | JWT claim to map to team memberships |

Multiple OIDC providers can be configured per organization — users choose their provider during login.

### Team Auto-Mapping

When the OIDC provider includes a groups/teams claim in the ID token, Astonish can automatically map those groups to internal team memberships using the `team_claim` field.

On each login, team memberships are reconciled — users are added to teams matching their group claims. This keeps access in sync with your identity provider without manual administration.

## CLI Login

Connect to a platform instance:

```bash
# Email/password login
astonish login https://astonish.example.com

# SSO/OIDC login (opens browser)
astonish login https://astonish.example.com --sso

# Specify org and team
astonish login https://astonish.example.com --org acme --team backend
```

The `--sso` flag initiates a device-code flow that opens the browser for identity provider authentication.

When multiple organizations or teams are available, the CLI prompts for interactive selection.

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
  jwt_secret: "$<JWT_SECRET>"

  # Built-in auth settings
  builtin:
    enabled: true
    password_min_length: 12
```

OIDC providers are managed through the platform API and stored per-organization in the database, not in the config file.

## See Also

- [Credential Security](./credential-security.md) — how credentials are protected after authentication
- [Audit Logging](./audit-logging.md) — authentication events are recorded in the audit trail
