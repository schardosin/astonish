---
title: Troubleshooting
description: Solve common issues with Astonish
sidebar:
  order: 5
---

# Troubleshooting

Common issues and how to solve them.

## Installation Issues

### Command Not Found

**Symptom:** `zsh: command not found: astonish`

**Solution:**

1. Verify installation:
```bash
which astonish
```

2. If not found, add to PATH:
```bash
# macOS/Linux
export PATH=$PATH:/usr/local/bin
```

3. Or reinstall:
```bash
brew install schardosin/astonish/astonish
```

### Permission Denied

**Symptom:** `permission denied` when running

**Solution:**
```bash
chmod +x /usr/local/bin/astonish
```

## Provider Issues

### API Key Not Found

**Symptom:** `Error: API key not configured`

**Solution:**

1. Check current config:
```bash
astonish config show
```

2. Re-run setup:
```bash
astonish setup
```

3. Or edit manually:
```bash
astonish config edit
```

Add:
```yaml
providers:
  openrouter:
    api_key: sk-or-v1-xxx
```

### Invalid API Key

**Symptom:** `Error: 401 Unauthorized`

**Solution:**

1. Verify key is correct (no extra spaces)
2. Check key hasn't expired
3. Confirm key has required permissions

### Rate Limited

**Symptom:** `Error: 429 Too Many Requests`

**Solution:**

1. Wait and retry
2. Use a different model
3. Consider a paid tier

### Model Not Found

**Symptom:** `Error: Model not found`

**Solution:**

1. Check model name spelling
2. Verify model is available for your provider
3. Try listing available models from provider docs

## MCP Issues

### Server Not Starting

**Symptom:** Tool not appearing in `astonish tools list`

**Solution:**

1. Test the command directly:
```bash
npx -y tavily-mcp@latest
```

2. Check for missing dependencies
3. Verify the `mcp_config.json` syntax:
```bash
astonish tools edit
```

### Missing Environment Variables

**Symptom:** `Error: API_KEY not set`

**Solution:**

Add to your MCP config:
```json
"env": {
  "API_KEY": "your-key-here"
}
```

### Tool Call Fails

**Symptom:** `Error: Tool execution failed`

**Solution:**

1. Run with debug:
```bash
astonish flows run my_flow -debug
```

2. Check tool input requirements
3. Verify the MCP server is running

## Flow Issues

### Flow Not Found

**Symptom:** `Error: Flow 'name' not found`

**Solution:**

1. List available flows:
```bash
astonish flows list
```

2. Check file location:
```bash
ls ~/.astonish/agents/
```

3. Verify file extension is `.yaml`

### YAML Syntax Error

**Symptom:** `Error: YAML parsing failed`

**Solution:**

1. Validate YAML syntax online or with:
```bash
python -c "import yaml; yaml.safe_load(open('flow.yaml'))"
```

2. Common issues:
   - Incorrect indentation
   - Missing quotes around special characters
   - Tabs instead of spaces

### Variable Not Found

**Symptom:** `{variable}` appears literally in output

**Solution:**

1. Check variable is defined by previous node
2. Verify spelling matches exactly
3. Ensure the defining node executed successfully

### Infinite Loop

**Symptom:** Flow never ends

**Solution:**

1. Check edge conditions
2. Ensure loop has exit condition
3. Look for circular references without conditions

## Studio Issues

### Won't Start

**Symptom:** `astonish studio` hangs or errors

**Solution:**

1. Try a different port:
```bash
astonish studio -port 8080
```

2. Check if port is in use:
```bash
lsof -i :9393
```

3. Kill existing process and retry

### Flow Not Saving

**Symptom:** Changes not persisted

**Solution:**

1. Press Cmd+S / Ctrl+S explicitly
2. Check file permissions:
```bash
ls -la ~/.astonish/agents/
```

3. Check disk space

### Node Editor Not Opening

**Symptom:** Clicking node does nothing

**Solution:**

1. Try double-clicking
2. Refresh the browser
3. Check browser console for errors (F12)

## Debug Mode

For any issue, enable debug mode:

```bash
astonish flows run my_flow -debug
```

This shows:
- Full error messages
- State at each step
- Tool call details
- Condition evaluations

## Getting Help

If you're still stuck:

1. **Check GitHub Issues:** [github.com/schardosin/astonish/issues](https://github.com/schardosin/astonish/issues)
2. **Open a New Issue:** Include debug output and config (redact API keys!)
3. **Check Version:** `astonish --version`

## Common Error Messages

| Error | Cause | Solution |
|-------|-------|----------|
| `API key not configured` | Missing provider setup | Run `astonish setup` |
| `401 Unauthorized` | Invalid API key | Check/update key |
| `429 Too Many Requests` | Rate limited | Wait or upgrade |
| `Model not found` | Wrong model name | Check provider docs |
| `Flow not found` | Missing file | Check `~/.astonish/agents/` |
| `YAML parsing failed` | Syntax error | Validate YAML |
| `Tool not found` | MCP not configured | Run `astonish tools list` |
