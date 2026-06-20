# Web & HTTP Tools

Two tools for fetching web content and making HTTP API requests.

## Tools

| Tool | Description | Confirmation |
|------|-------------|-------------|
| `web_fetch` | Fetch and extract content from a URL | auto-approve |
| `http_request` | Make arbitrary HTTP requests with full control | always-confirm (for mutating methods) |

## web_fetch

Retrieves a web page and extracts readable content (strips navigation, ads, scripts):

```
web_fetch:
  url: "https://docs.example.com/api/reference"
  format: "markdown"
```

Options:
- `format`: `markdown` (default), `text`, or `html`
- `timeout`: Request timeout in seconds (default 30)
- `headers`: Optional custom headers

GET requests via `web_fetch` are auto-approved. The tool is read-only and does not modify external state.

## http_request

Full-featured HTTP client for API interactions:

```
http_request:
  method: "POST"
  url: "https://api.example.com/deployments"
  headers:
    Content-Type: "application/json"
    Authorization: "Bearer ${API_TOKEN}"
  body: |
    {"environment": "staging", "ref": "main"}
```

Features:
- All HTTP methods (GET, POST, PUT, PATCH, DELETE)
- Custom headers and body
- Response includes status code, headers, and body
- Timeout configuration
- Credential injection (see below)

### Credential Injection

When used with [stored credentials](./credentials.md), the agent can inject secrets into requests without exposing them in conversation:

```
http_request:
  method: "GET"
  url: "https://api.service.com/data"
  credential: "service-api-key"
```

The credential value is injected at execution time and redacted from all logs and outputs.

See [Credentials](./credentials.md) for secure secret management and [Browser Automation](./browser.md) for JavaScript-rendered pages.
