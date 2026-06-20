# Flow Editor

The Flow Editor provides a visual drag-and-drop interface for designing multi-step agent pipelines. Flows define sequences of agent actions, tool calls, and logic that execute as a unit.

<!-- IMAGE: Flow editor canvas showing connected nodes -->

## Canvas

The flow canvas is an infinite, pannable workspace where you place and connect nodes. Each node represents a step in the flow — an agent call, a tool invocation, a conditional branch, or a data transformation.

### Adding Nodes

- **Drag from palette** — The left sidebar contains available node types
- **Right-click** — Context menu to insert a node at the cursor position
- **AI Assist** — Describe what you want and let AI generate nodes (see below)

### Drawing Connections

Click an output port on one node and drag to an input port on another to create a connection. Connections define execution order and data flow between steps.

## Node Types

| Type | Description |
|------|-------------|
| **Agent** | Invoke the AI agent with a prompt |
| **Tool** | Execute a specific tool directly |
| **Condition** | Branch based on a value or expression |
| **Transform** | Manipulate data between steps |
| **Input** | Flow entry point (parameters) |
| **Output** | Flow result |

## AI Assist

Click the **AI Assist** button or press `Ctrl+G` to open the generation prompt. Describe the node or sub-flow you need in natural language:

```
"Add a node that reads a CSV file and extracts the email column"
```

AI Assist generates the appropriate nodes and connections, placing them on the canvas for review before you commit them to the flow.

## Execution Mode

Click **Run** (or press `F5`) to execute the flow. The editor switches to execution mode:

- Active nodes highlight as they execute
- Output appears in the panel below each node
- Errors display inline with stack traces
- The full execution log is available in the right panel

## Saving and Versioning

Flows are saved automatically as you edit. Each save creates a version you can roll back to. Flows are stored in your Astonish config and can be exported as YAML for version control.

```bash
# Run a flow from CLI
astonish flows run my-flow --input '{"file": "data.csv"}'
```
