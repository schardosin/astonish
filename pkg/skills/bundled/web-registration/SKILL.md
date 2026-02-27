---
name: web-registration
description: "Register accounts on websites using browser automation, agent identity, and email verification"
---

# Web Registration

Step-by-step procedure for registering an account on a website using the agent's configured identity, browser tools, and email verification.

## Prerequisites

- Browser tools must be available
- Agent identity should be configured in `config.yaml` under `agent_identity`
- Email tools should be configured for receiving verification emails

## Registration Flow

### 1. Navigate to Registration Page
```
browser_navigate url="https://example.com/signup"
browser_snapshot
```
Identify the registration form fields from the snapshot.

### 2. Fill Registration Form
Use the agent identity fields:
- Name field: use `agent_identity.name`
- Username field: use `agent_identity.username`
- Email field: use `agent_identity.email` (must match your IMAP-configured email)
- Bio/About field: use `agent_identity.bio` (if present)
- Website field: use `agent_identity.website` (if present)

```
browser_fill_form fields=[
  {"ref": "refN", "value": "<name>"},
  {"ref": "refM", "value": "<email>"},
  {"ref": "refK", "value": "<username>"}
]
```

For password fields, generate a strong random password (16+ chars, mixed case, digits, symbols), then save it to the credential store BEFORE submitting:
```
save_credential name="example-com" type="password" username="<username>" password="<generated>"
```

### 3. Handle CAPTCHA
If a CAPTCHA is present on the registration form:
```
browser_request_human reason="Please solve the CAPTCHA on the registration form and click Submit"
```
Wait for the user to complete the CAPTCHA. After handoff completes, take a fresh snapshot:
```
browser_snapshot
```

### 4. Submit and Check Result
```
browser_click ref=<submit_button_ref>
browser_snapshot
```
Check for success messages or error states (username taken, email already registered, etc.).

If the username is taken, try variations:
- `username_01`, `username_02`
- `username2025`
- `usernameAI`

### 5. Email Verification
Most sites send a verification email after registration:
```
email_wait sender="example.com" subject="verify" timeout_seconds=120
```
This polls the inbox waiting for a matching email. Once received:
```
email_read id=<message_id>
```
Extract the verification link from the email body, then:
```
browser_navigate url="<verification_link>"
browser_snapshot
```
Confirm the verification succeeded.

### 6. Save Account Details
After successful registration, save the account to persistent memory:
```
memory_save section="Accounts" content="## example.com\n- Username: <username>\n- Email: <email>\n- Credential: example-com (in credential store)\n- Registered: <date>"
```

## Username Collision Strategy

When the preferred username is taken:
1. Try appending `_01`, `_02`, etc.
2. Try appending the current year
3. Try prepending or appending `ai` or `bot`
4. Update the saved credential and memory entry with the actual username used

## Common Pitfalls

- **Forgot to save password before submitting**: Always save to credential store first
- **Email verification timeout**: Increase timeout or check spam folder with `email_search`
- **JavaScript-heavy forms**: Use `browser_snapshot` after each interaction to verify state changes
- **Terms of Service checkbox**: Look for checkbox elements in the snapshot and click them
- **Two-step registration**: Some sites split registration across multiple pages; snapshot after each step
