# Browser Automation

Astonish includes 32 browser automation tools powered by Chrome DevTools Protocol (CDP) in pure Go. No Node.js, Puppeteer, or Playwright dependencies required.

## Architecture

The browser tools communicate directly with Chrome/Chromium via CDP WebSocket connections. Features:

- Pure Go implementation (no external runtime)
- Headless and headed modes
- Stealth mode to avoid bot detection
- Device emulation for mobile testing
- Full page interaction (click, type, scroll, drag)

## Configuration

```yaml
browser:
  headless: true
  stealth: true
  timeout: 30
  viewport:
    width: 1280
    height: 720
  user_data_dir: ""
  executable: ""         # Auto-detected if empty
```

## Tool Categories

### Navigation (4 tools)

| Tool | Description |
|------|-------------|
| `browser_navigate` | Go to a URL |
| `browser_back` | Navigate back |
| `browser_forward` | Navigate forward |
| `browser_refresh` | Reload the page |

### Interaction (8 tools)

| Tool | Description |
|------|-------------|
| `browser_click` | Click an element by selector |
| `browser_type` | Type text into an input |
| `browser_select` | Select dropdown option |
| `browser_scroll` | Scroll page or element |
| `browser_hover` | Hover over an element |
| `browser_drag` | Drag and drop |
| `browser_press_key` | Press keyboard key |
| `browser_upload_file` | Upload file to input |

### Observation (8 tools)

| Tool | Description |
|------|-------------|
| `browser_screenshot` | Capture page screenshot |
| `browser_get_text` | Extract text from element |
| `browser_get_html` | Get element HTML |
| `browser_get_attribute` | Read element attribute |
| `browser_get_url` | Current page URL |
| `browser_get_title` | Current page title |
| `browser_query_selector` | Find elements by CSS selector |
| `browser_evaluate` | Execute JavaScript |

### Tab Management (4 tools)

| Tool | Description |
|------|-------------|
| `browser_new_tab` | Open a new tab |
| `browser_close_tab` | Close current tab |
| `browser_switch_tab` | Switch to a tab by index |
| `browser_list_tabs` | List all open tabs |

### Session (4 tools)

| Tool | Description |
|------|-------------|
| `browser_open` | Launch browser session |
| `browser_close` | Close browser session |
| `browser_cookies_get` | Read cookies |
| `browser_cookies_set` | Set cookies |

### Advanced (4 tools)

| Tool | Description |
|------|-------------|
| `browser_wait_for` | Wait for selector/condition |
| `browser_emulate_device` | Set device (iPhone, iPad, etc.) |
| `browser_network_intercept` | Intercept network requests |
| `browser_pdf` | Save page as PDF |

## Stealth Mode

When `stealth: true`, the browser applies anti-detection techniques:

- Removes `navigator.webdriver` flag
- Randomizes fingerprint signals
- Emulates human-like timing
- Spoofs plugin and language lists

## Device Emulation

Test mobile layouts without physical devices:

```
browser_emulate_device:
  device: "iPhone 15 Pro"
```

Built-in device profiles include common phones, tablets, and desktop resolutions.

## Example Workflow

```
1. browser_open
2. browser_navigate: "https://app.example.com/login"
3. browser_type: selector="#email", text="user@example.com"
4. browser_type: selector="#password", text="***"  (from credential_get)
5. browser_click: selector="button[type=submit]"
6. browser_wait_for: selector=".dashboard"
7. browser_screenshot
8. browser_close
```

See [Web & HTTP Tools](./web-http.md) for simpler page fetching and [Credentials](./credentials.md) for secure password handling in automation.
