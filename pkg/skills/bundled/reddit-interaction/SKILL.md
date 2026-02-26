---
name: reddit-interaction
description: "Register and interact with Reddit using browser automation and email verification"
---

# Reddit Interaction

Procedures for registering a Reddit account and performing common interactions (posting, commenting, voting, reading) using browser automation.

## Account Registration

### 1. Navigate to Reddit Signup
```
browser_navigate url="https://www.reddit.com/register"
browser_snapshot
```

### 2. Fill Registration
Reddit's signup flow typically has multiple steps:

**Step 1 — Email:**
```
browser_type ref=<email_field> text="<agent_identity.email>"
browser_click ref=<continue_button>
browser_snapshot
```

**Step 2 — Username and Password:**
Reddit may suggest a username. Use the agent identity username instead:
```
browser_type ref=<username_field> text="<agent_identity.username>"
browser_type ref=<password_field> text="<generated_password>"
```
Save the password to credential store BEFORE submitting:
```
save_credential name="reddit" type="password" username="<username>" password="<generated>"
```

**Step 3 — CAPTCHA:**
Reddit almost always presents a CAPTCHA during registration:
```
browser_request_human reason="Please solve the Reddit CAPTCHA and click the Sign Up button"
```

### 3. Email Verification
```
email_wait sender="reddit" subject="verify" timeout_seconds=180
email_read id=<message_id>
```
Extract verification link and navigate to it:
```
browser_navigate url="<verification_link>"
browser_snapshot
```

### 4. Save Account
```
memory_save section="Accounts" content="## Reddit\n- Username: u/<username>\n- Email: <email>\n- Credential: reddit (in credential store)\n- Registered: <date>"
```

## Common Actions

### Read a Subreddit
```
browser_navigate url="https://www.reddit.com/r/<subreddit>"
browser_snapshot
```
Use `mode="efficient"` for large subreddit pages.

### Read a Post and Comments
```
browser_navigate url="https://www.reddit.com/r/<subreddit>/comments/<post_id>/<slug>"
browser_snapshot
```

### Create a Post
```
browser_navigate url="https://www.reddit.com/r/<subreddit>/submit"
browser_snapshot
browser_type ref=<title_field> text="<post_title>"
browser_type ref=<body_field> text="<post_body>"
browser_click ref=<post_button>
```

### Comment on a Post
Navigate to the post first, then:
```
browser_snapshot
browser_type ref=<comment_box> text="<comment_text>"
browser_click ref=<comment_button>
```

### Vote
```
browser_click ref=<upvote_button>
```
or
```
browser_click ref=<downvote_button>
```

## Reddit-Specific Notes

- **New Account Restrictions**: New Reddit accounts have rate limits (posting frequency, karma thresholds for some subreddits). Start with commenting before posting.
- **Old vs New Reddit**: Use `www.reddit.com` (new design) for browser automation; the layout is more consistent.
- **Login persistence**: The browser uses a persistent profile, so Reddit sessions survive across restarts.
- **Popup modals**: Reddit shows many modals (app install, cookie consent, NSFW warnings). Dismiss them by looking for close/dismiss buttons in the snapshot.
- **Infinite scroll**: Reddit uses infinite scroll. If content is not visible, scroll down with `browser_press_key key="End"` and re-snapshot.
