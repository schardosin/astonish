---
title: Web & HTTP Tools
description: Fetch web pages and make API calls
---

The Web & HTTP category includes 2 tools for fetching web content and making API requests with full control over authentication.

## web_fetch

Fetch a URL and extract content as markdown, text, or HTML. This is the preferred tool for reading web content — it is fast, free, and requires no API key.

For JavaScript-heavy pages that return empty content, use the [browser tools](/tools/browser) instead.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `url` | string | Yes | HTTP or HTTPS URL to fetch |
| `mode` | string | No | Extraction mode: `markdown` (default), `readable`, or `raw` |
| `max_chars` | int | No | Max characters to return (default: 50000) |

## http_request

Make HTTP requests with full control over method, headers, body, and authentication. Set `credential` to a stored credential name for automatic auth header injection.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `url` | string | Yes | URL to send the request to |
| `method` | string | No | HTTP method (default: GET) |
| `headers` | object | No | Additional HTTP headers |
| `body` | string | No | Request body |
| `credential` | string | No | Stored credential name for auth |
| `timeout` | int | No | Timeout in seconds (default: 30, max: 120) |
| `max_bytes` | int | No | Max response size (default: 2MB) |

## When to use which

| Use case | Tool |
|----------|------|
| Read an article, docs page, or blog post | `web_fetch` |
| Download and parse a web page | `web_fetch` |
| Call a REST API | `http_request` |
| Send data with POST/PUT/PATCH | `http_request` |
| Make an authenticated request | `http_request` with `credential` |
| Interact with a JavaScript-heavy SPA | Browser tools |

## Credential integration

The `credential` parameter on `http_request` connects to the [credential store](/tools/credentials), allowing authenticated requests without exposing secrets in tool calls.

To use it, pass the name of a stored credential:

```json
{
  "url": "https://api.example.com/data",
  "method": "GET",
  "credential": "my-api-key"
}
```

The appropriate authorization header is injected automatically based on the credential type:

| Credential type | Header format |
|----------------|---------------|
| API key | `Authorization: ApiKey <key>` or custom header |
| Bearer token | `Authorization: Bearer <token>` |
| Basic auth | `Authorization: Basic <base64(user:pass)>` |
| OAuth | `Authorization: Bearer <access_token>` |

For OAuth credentials, token refresh is handled automatically when the access token expires.

## Security

`http_request` restricts outbound requests to **private IP ranges** by default to prevent server-side request forgery (SSRF):

- `10.0.0.0/8`
- `172.16.0.0/12`
- `192.168.0.0/16`
- `127.0.0.0/8` (localhost)

This restriction means `http_request` can be used to access internal services running on your local network or machine. Requests to public internet addresses are allowed without restriction.
