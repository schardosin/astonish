---
name: hackernews-interaction
description: "Register and interact with Hacker News (Y Combinator) using browser automation"
---

# Hacker News Interaction

Procedures for registering a Hacker News account and performing common interactions using browser automation. Hacker News has a simple HTML interface that works well with browser tools.

## Account Registration

### 1. Navigate to Signup
```
browser_navigate url="https://news.ycombinator.com/login?goto=news"
browser_snapshot
```
The login page has both login and create-account sections.

### 2. Create Account
Find the "Create Account" section (typically at the bottom of the login page):
```
browser_type ref=<create_username_field> text="<agent_identity.username>"
browser_type ref=<create_password_field> text="<generated_password>"
```
Save the password to credential store BEFORE submitting:
```
save_credential name="hackernews" type="password" username="<username>" password="<generated>"
```
```
browser_click ref=<create_button>
browser_snapshot
```

### 3. Verify Registration
HN does not require email verification for account creation, but you should add an email in settings for account recovery:
```
browser_navigate url="https://news.ycombinator.com/user?id=<username>"
browser_snapshot
```
Find the email field and set it:
```
browser_type ref=<email_field> text="<agent_identity.email>"
browser_click ref=<update_button>
```

### 4. Save Account
```
memory_save section="Accounts" content="## Hacker News\n- Username: <username>\n- Profile: https://news.ycombinator.com/user?id=<username>\n- Credential: hackernews (in credential store)\n- Registered: <date>"
```

## Common Actions

### Read Front Page
```
browser_navigate url="https://news.ycombinator.com"
browser_snapshot
```
Stories are listed with rank, title, points, submitter, and comment count.

### Read a Story and Comments
```
browser_navigate url="https://news.ycombinator.com/item?id=<item_id>"
browser_snapshot
```
For long comment threads, use `mode="efficient"` or scroll to load more.

### Submit a Story
```
browser_navigate url="https://news.ycombinator.com/submit"
browser_snapshot
browser_type ref=<title_field> text="<story_title>"
browser_type ref=<url_field> text="<story_url>"
browser_click ref=<submit_button>
```
For text posts (Ask HN, etc.), leave the URL field empty and fill the text field instead.

### Comment on a Story
Navigate to the story page, then find the comment textarea:
```
browser_snapshot
browser_type ref=<comment_box> text="<comment_text>"
browser_click ref=<add_comment_button>
```

### Reply to a Comment
Click the "reply" link on the comment first:
```
browser_click ref=<reply_link>
browser_snapshot
browser_type ref=<reply_box> text="<reply_text>"
browser_click ref=<reply_button>
```

### Upvote
```
browser_click ref=<upvote_triangle>
```
Note: HN only allows upvoting (no downvoting until 500+ karma).

### Search
```
browser_navigate url="https://hn.algolia.com/?q=<search_query>"
browser_snapshot
```
Algolia-powered search provides better results than HN's built-in search.

## Hacker News-Specific Notes

- **Simple HTML**: HN uses minimal JavaScript, making it very reliable with browser automation. `browser_snapshot` captures everything accurately.
- **Rate Limits**: HN has strict rate limits for new accounts. Posting and commenting too frequently triggers temporary bans. Space out interactions.
- **Karma System**: New accounts start with 1 karma. Some features require karma thresholds (downvoting: 500+, flagging: 30+).
- **No CAPTCHA**: HN does not typically use CAPTCHAs, making registration straightforward.
- **Formatting**: Comments support limited formatting (blank line for paragraph break, asterisks for italic, code blocks with 2-space indent). No markdown.
- **Login Sessions**: The browser persistent profile keeps HN sessions alive across restarts.
- **Show HN / Ask HN**: Prefix titles with "Show HN: " or "Ask HN: " for the respective categories.
