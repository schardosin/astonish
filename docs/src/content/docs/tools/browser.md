---
title: "Browser Automation"
description: "32 native browser tools for web automation, scraping, and testing"
---

Astonish includes 32 native browser automation tools powered by [Rod](https://go-rod.github.io/) (Chrome DevTools Protocol). No external dependencies required — the browser is managed automatically.

**Key features:**

- Stealth anti-detection mode
- Persistent browser profiles
- Device emulation (mobile, tablet, desktop)
- Incognito tabs
- Screenshot compression
- Accessibility tree snapshots with element ref IDs
- Human-in-the-loop handoff for CAPTCHAs and MFA

## Configuration

Configure the browser through **Studio Settings > Browser**, or use the interactive CLI:

```bash
astonish config browser
```

This lets you choose between the default engine, CloakBrowser, or a custom configuration.

For manual configuration, edit `config.yaml` under the `browser:` section:

```yaml
browser:
  headless: false            # Run headless (default: false)
  viewport_width: 1280       # Viewport width in pixels
  viewport_height: 720       # Viewport height in pixels
  chrome_path: ""            # Custom Chromium binary (empty = auto-download)
  user_data_dir: ""          # Persistent profile directory
  navigation_timeout: 30s    # Page load timeout
  proxy: ""                  # HTTP/SOCKS proxy URL
  remote_cdp_url: ""         # Connect to external/anti-detect browsers
  fingerprint_seed: ""       # CloakBrowser fingerprint seed
  fingerprint_platform: ""   # CloakBrowser fingerprint platform
```

For the full list of browser configuration options, see the [Config File Reference](/astonish/configuration/config-reference/).

## Navigation

| Tool | Description | Parameters |
|------|-------------|------------|
| `browser_navigate` | Navigate to a URL | `url` |
| `browser_navigate_back` | Go back in browser history | — |

## Interaction

| Tool | Description | Parameters |
|------|-------------|------------|
| `browser_click` | Click an element by ref | `ref`, `button`, `doubleClick` |
| `browser_type` | Type text into an input field | `ref`, `text`, `submit`, `slowly` |
| `browser_hover` | Hover over an element | `ref` |
| `browser_drag` | Drag and drop between elements | `startRef`, `endRef` |
| `browser_press_key` | Press a keyboard key | `key` |
| `browser_select_option` | Select from a dropdown | `ref`, `values` |
| `browser_fill_form` | Fill multiple form fields at once | `fields` (array of `{ref, value}`) |
| `browser_file_upload` | Upload files to a file input | `ref`, `paths` |
| `browser_handle_dialog` | Handle JS alerts, confirms, and prompts | `accept`, `promptText`, `timeout_ms` |

## Observation

| Tool | Description | Parameters |
|------|-------------|------------|
| `browser_snapshot` | Accessibility tree snapshot with ref IDs for interaction. The primary tool for understanding page content. | `mode` (`full` or `efficient`), `interactive`, `compact`, `maxDepth`, `selector`, `frame`, `maxChars` |
| `browser_take_screenshot` | Capture a visual screenshot | `fullPage`, `ref`, `selector`, `format` |
| `browser_console_messages` | Read browser console logs | `level`, `clear` |
| `browser_network_requests` | View network activity | `urlFilter`, `clear` |

## Tab Management

| Tool | Description | Parameters |
|------|-------------|------------|
| `browser_tabs` | List, open, close, or select tabs | `action`, `url`, `targetId`, `incognito` |
| `browser_close` | Close the current page/tab | — |
| `browser_resize` | Resize the viewport | `width`, `height` |
| `browser_wait_for` | Wait for a condition: text, selector, URL, page state, or JS expression | `text`, `textGone`, `selector`, `url`, `timeout`, `state`, `expression` |

## Advanced

| Tool | Description | Parameters |
|------|-------------|------------|
| `browser_evaluate` | Run a JavaScript expression | `expression`, `ref` |
| `browser_run_code` | Run multi-line JS with async/await support | `code` |
| `browser_pdf` | Save the current page as a PDF | `path` |
| `browser_response_body` | Intercept HTTP response bodies | `action`, `urlPattern`, `timeout`, `maxChars` |

`browser_response_body` follows a listen-trigger-read-stop workflow:

1. Call with `action: "listen"` and a `urlPattern` to start intercepting.
2. Trigger the request (e.g., click a button or navigate).
3. Call with `action: "read"` to retrieve the response body.
4. Call with `action: "stop"` to clean up.

## State and Emulation

| Tool | Description | Parameters |
|------|-------------|------------|
| `browser_cookies` | Get, set, or clear cookies | `action`, `name`, `value`, `url`, `domain`, etc. |
| `browser_storage` | Read/write localStorage or sessionStorage | `action`, `kind`, `key`, `value` |
| `browser_set_offline` | Simulate offline mode | `offline` |
| `browser_set_headers` | Add extra HTTP headers to all requests | `headers` |
| `browser_set_credentials` | Set HTTP Basic Auth credentials | `username`, `password` |
| `browser_set_geolocation` | Override browser geolocation | `latitude`, `longitude`, `accuracy` |
| `browser_set_media` | Set preferred color scheme | `colorScheme` |
| `browser_set_timezone` | Override the browser timezone | `timezone` |
| `browser_set_locale` | Override the browser locale | `locale` |
| `browser_set_device` | Emulate a mobile or tablet device | `device`, `landscape` |

`browser_set_device` supports common devices including iPhone, iPad, Pixel, and Galaxy models.

## Human-in-the-Loop

| Tool | Description | Parameters |
|------|-------------|------------|
| `browser_request_human` | Start a browser handoff for CAPTCHAs, MFA, or payment forms. Returns CDP connection instructions for the user. | `reason`, `timeout_seconds` |
| `browser_handoff_complete` | Wait for the user to finish the handoff | `timeout_seconds` |

## Common Workflow

A typical browser automation session follows this pattern:

1. **Navigate** to a page with `browser_navigate`.
2. **Understand** the page with `browser_snapshot` — this returns an accessibility tree with ref IDs.
3. **Interact** with elements using the ref IDs from the snapshot (click, type, select, etc.).
4. **Extract** data from the page or take screenshots with `browser_take_screenshot`.
5. **Hand off** to a human for CAPTCHAs or MFA via `browser_request_human`, then wait with `browser_handoff_complete`.
