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

![Run Button](/astonish/images/studio-flow_run.webp)
*The Run Dialog*

### Execution Flow

1. Flow starts at the **START** node
2. Each node executes in sequence
3. Node output appears in the chat panel
4. Branches follow their conditions
5. Flow ends at the **END** node

## The Chat Panel

The right panel shows execution in real-time:

![Chat Panel](/astonish/images/studio-flow_run_execution.webp)
*The chat panel during execution*

### What You'll See

| Element | Meaning |
|---------|---------|
| **Execution started...** | Flow has begun |
| **Agent** | Prompts from Input nodes or responses from LLM nodes |
| **User** | Your input responses |
| **✦ Executing Node: [name]** | Shows which node is currently running |
| **Start Again** | Button to restart the flow from the beginning |

## Interactive Input

When the flow reaches an **Input** node:

1. The chat panel prompts you
2. Type your response
3. Press **Enter** to continue

If the Input node has **Options**, each option appears as a button you can click.



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



## Tips for Debugging

1. **Add Output nodes** — Display intermediate values to see what's happening
2. **Test simple flows first** — Verify nodes work individually before building complex flows
3. **Check the YAML** — Click **View Source** to see the raw flow definition
4. **Use CLI debug mode** — Run `astonish flows run --debug <name>` for verbose output

## Next Steps

- **[Keyboard Shortcuts](/studio/keyboard-shortcuts/)** — Speed up your workflow
- **[Exporting & Sharing](/studio/exporting-sharing/)** — Share your flows
