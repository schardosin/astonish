---
# Browser Automation

Astonish includes 34 browser automation tools for full web interaction, testing, and data extraction.

## Configuration

```yaml
browser:
  headless: true
  stealth: true
  timeout: 30
  viewport:
    width: 1280
    height: 720
```

Configure browser settings via `astonish config edit` or Studio Settings.

## Tools

### Navigation

| Tool | Description |
|------|-------------|
| `browser_navigate` | Go to a URL |
| `browser_navigate_back` | Navigate back |

### Interaction

| Tool | Description |
|------|-------------|
| `browser_click` | Click an element |
| `browser_type` | Type text into an input |
| `browser_hover` | Hover over an element |
| `browser_drag` | Drag and drop |
| `browser_press_key` | Press a keyboard key |
| `browser_select_option` | Select a dropdown option |
| `browser_fill_form` | Fill multiple form fields at once |
| `browser_file_upload` | Upload a file to an input |
| `browser_handle_dialog` | Accept/dismiss browser dialogs |

### Observation

| Tool | Description |
|------|-------------|
| `browser_snapshot` | Get page accessibility snapshot (structured content) |
| `browser_take_screenshot` | Capture page screenshot |
| `browser_console_messages` | Read browser console output |
| `browser_network_requests` | View network request log |
| `browser_response_body` | Get response body for a network request |
| `browser_evaluate` | Execute JavaScript in page context |
| `browser_run_code` | Run JavaScript with return value |

### Tab & Window Management

| Tool | Description |
|------|-------------|
| `browser_tabs` | List all open tabs |
| `browser_close` | Close browser or tab |
| `browser_resize` | Resize browser viewport |
| `browser_wait_for` | Wait for a selector or condition |
| `browser_pdf` | Save page as PDF |

### Cookies & Storage

| Tool | Description |
|------|-------------|
| `browser_cookies` | Get/set cookies |
| `browser_storage` | Access localStorage/sessionStorage |

### Environment Configuration

| Tool | Description |
|------|-------------|
| `browser_set_offline` | Simulate offline mode |
| `browser_set_headers` | Set custom request headers |
| `browser_set_credentials` | Set HTTP Basic Auth credentials |
| `browser_set_geolocation` | Override geolocation |
| `browser_set_media` | Set media features (dark mode, etc.) |
| `browser_set_timezone` | Override timezone |
| `browser_set_locale` | Override locale |
| `browser_set_device` | Emulate a device (iPhone, iPad, etc.) |

### Human Interaction

| Tool | Description |
|------|-------------|
| `browser_request_human` | Request human intervention for CAPTCHAs, etc. |

## Example Workflow

```
1. browser_navigate: "https://app.example.com/login"
2. browser_type: selector="#email", text="user@example.com"
3. browser_type: selector="#password", text="***"  (from resolve_credential)
4. browser_click: selector="button[type=submit]"
5. browser_wait_for: selector=".dashboard"
6. browser_take_screenshot
7. browser_close
```

## Stealth Mode

When `stealth: true`, the browser applies anti-detection techniques to avoid bot detection.

See [Web & HTTP Tools](./web-http.md) for simpler page fetching and [Credentials](./credentials.md) for secure password handling in automation.
