# Browser Automation

Astonish includes 34 browser automation tools for full web interaction, testing, and data extraction.

## Configuration

```yaml
browser:
  headless: true
  stealth: true
  timeout: 30
  viewport_width: 1920
  viewport_height: 1080
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
| `browser_click` | Click an element (`animate_cursor=true` moves the demo cursor first) |
| `browser_type` | Type text into an input |
| `browser_hover` | Hover over an element |
| `browser_drag` | Drag and drop |
| `browser_press_key` | Press a keyboard key |
| `browser_select_option` | Select a dropdown option |
| `browser_fill_form` | Fill multiple form fields at once |
| `browser_highlight` | Draw a visible highlight overlay (ref or CSS selector) |
| `browser_clear_highlights` | Remove highlight overlays |
| `browser_move_cursor` | Move the visible demo cursor to a ref, selector, or x/y |
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
| `browser_fullscreen` | Enter/exit Chromium window fullscreen (recording prep) |
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

### Session Recording

| Tool | Description |
|------|-------------|
| `browser_start_recording` | Start recording the browser display to an MP4 (ffmpeg x11grab); returns path + capture width/height |
| `browser_stop_recording` | Stop recording and finalize the MP4 (emitted as a session artifact) |
| `browser_recording_status` | Check whether a recording is in progress |

Recording captures the **live X display** (probed via `xdpyinfo`). KasmVNC is locked against client resize with `desktop.resolution` set to the configured viewport (default **1920×1080**) so Studio’s VNC view cannot shrink the framebuffer. Call `browser_start_recording` before a scripted demo, then `browser_stop_recording` when finished. Local mode needs `ffmpeg` + `xdpyinfo`; sandboxes include them with the browser image.

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

### Recording example

```
1. browser_navigate: "https://portal.example.com"   # warm-up first (avoid blank-tab capture)
2. browser_fullscreen: enabled=true
3. browser_start_recording: filename="portal-demo.mp4"
4. browser_highlight: ref=…, label="Dashboard"
5. browser_click: ref=…, animate_cursor=true
6. browser_stop_recording
```

For tutorial drills, `run_drill` starts each `record: segment` **before** the step tool — keep open/fullscreen steps without `narration`/`record`.

## Stealth Mode

When `stealth: true`, the browser applies anti-detection techniques to avoid bot detection.

See [Web & HTTP Tools](./web-http.md) for simpler page fetching and [Credentials](./credentials.md) for secure password handling in automation.
