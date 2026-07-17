# Web & HTTP Tools

Three tools for fetching web content, reading PDFs, and making HTTP API requests.

## Tools

| Tool | Description | Confirmation |
|------|-------------|-------------|
| `web_fetch` | Fetch and extract content from a URL | auto-approve |
| `read_pdf` | Extract text content from a PDF file | auto-approve |
| `http_request` | Make arbitrary HTTP requests with full control | always-confirm (mutating methods) |

## web_fetch

Retrieves a web page and extracts readable content (strips navigation, ads, scripts):

```
web_fetch:
  url: "https://docs.example.com/api/reference"
```

Returns clean, readable text extracted from the page. Auto-approved since it's read-only.

::: warning Private Networks
`web_fetch` and `http_request` cannot reach private/RFC1918 IPs (192.168.x.x, 10.x.x.x, 172.16-31.x.x) or localhost. Use `shell_command` with `curl` for private network endpoints.
:::

## read_pdf

Extracts text content from a PDF file (local path or URL):

```
read_pdf:
  source: "/path/to/document.pdf"
```

## http_request

Full-featured HTTP client for API interactions:

```
http_request:
  method: "POST"
  url: "https://api.example.com/deployments"
  headers:
    Content-Type: "application/json"
  body: '<"environment": "staging", "ref": "main">'
```

Features:
- All HTTP methods (GET, POST, PUT, PATCH, DELETE, HEAD, OPTIONS)
- Custom headers and body
- `body_path` — send a sandbox file as the raw request body (binary-safe; use for MP4/asset uploads)
- `multipart` — build `multipart/form-data` with text fields and file parts (prefer this over `curl` for Synthesia-style uploads)
- `body`, `body_path`, and `multipart` are mutually exclusive
- Response includes status code, headers, and body
- JSON Content-Type set automatically when string `body` starts with `{` or `[`

### Credential Injection

When used with [stored credentials](./credentials.md), secrets are injected automatically:

```
http_request:
  method: "GET"
  url: "https://api.service.com/data"
  credential: "service-api-key"
```

The credential value is injected at execution time and redacted from all logs and outputs.

See [Credentials](./credentials.md) for secure secret management and [Browser Automation](./browser.md) for JavaScript-rendered pages.
