---
title: Running & Debugging
description: Execute flows and debug issues in Astonish Studio
sidebar:
  order: 5
---

# Running & Debugging

Studio provides real-time execution and debugging tools to test your flows before deploying them.

## Running a Flow

### Start Execution

1. Open a flow in the canvas
2. Click the **▶ Run** button in the top bar
3. The chat panel opens on the right

![Run Button](/src/assets/placeholder.png)
*The Run button in the top bar*

### Execution Flow

1. Flow starts at the **START** node
2. Each node executes in sequence
3. Node output appears in the chat panel
4. Branches follow their conditions
5. Flow ends at the **END** node

## The Chat Panel

The right panel shows execution in real-time:

![Chat Panel](/src/assets/placeholder.png)
*The chat panel during execution*

### What You'll See

| Element | Meaning |
|---------|---------|
| **AI:** | LLM node response |
| **User:** | Your input (for Input nodes) |
| **Tool:** | Tool call results |
| **Node: [name]** | Current node being executed |

## Interactive Input

When the flow reaches an **Input** node:

1. The chat panel prompts you
2. Type your response
3. Press **Enter** to continue

If the Input node has **Options**, you'll see a dropdown instead.

## Debug Mode

Enable debug mode for verbose output:

1. Click the **Settings** icon (⚙️) next to Run
2. Toggle **Debug Mode** on
3. Run the flow

### Debug Information

Debug mode shows:
- Node execution order
- State changes between nodes
- Tool call inputs and responses
- Condition evaluations

![Debug Output](/src/assets/placeholder.png)
*Verbose debug output*

## Common Issues

### Node Not Executing

**Symptom:** Flow skips a node or stops unexpectedly.

**Check:**
- Is the node connected to the flow?
- Are incoming edges properly connected?
- Is a condition blocking execution?

### Wrong Branch Taken

**Symptom:** Flow goes to unexpected path.

**Check:**
- Verify condition syntax
- Print the variable being checked
- Check for typos in variable names

### AI Not Responding

**Symptom:** LLM node hangs or errors.

**Check:**
- Is your provider configured correctly?
- Is the API key valid?
- Check provider status page

### Tool Call Fails

**Symptom:** Tool returns an error.

**Check:**
- Is the MCP server running?
- Are required parameters provided?
- Check the tool's input requirements

## Stopping Execution

To stop a running flow:
- Click the **■ Stop** button
- Or close the chat panel

## Re-running

To run again:
1. Click **▶ Run** again
2. Previous state is cleared
3. Execution starts fresh from START

## Viewing State

During execution, you can inspect the current state:

1. Enable Debug Mode
2. State is printed after each node

Example:
```
[State] {
  "user_question": "What is AI?",
  "analysis": "AI is...",
  "confidence": 0.95
}
```

## Tips for Debugging

1. **Add Output nodes** — Print intermediate values
2. **Use simple flows first** — Test nodes individually
3. **Check the YAML** — Sometimes visual issues hide there
4. **Enable Debug** — When in doubt, verbose mode helps

## Next Steps

- **[Keyboard Shortcuts](/studio/keyboard-shortcuts/)** — Speed up your workflow
- **[Exporting & Sharing](/studio/exporting-sharing/)** — Share your flows
