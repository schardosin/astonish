# Flow Editor

The Flow Editor provides a visual interface for designing multi-step agent pipelines. Flows define sequences of agent actions, tool calls, and logic that execute as a unit.

## Canvas

The flow canvas is an infinite, pannable workspace where you place and connect nodes. Each node represents a step in the flow — an LLM call, a tool invocation, user input, or a state update.

### Adding Nodes

- **Toolbar** — Click node type buttons in the top-right panel to add nodes
- **Plus button** — Click the "+" connector on an existing node to add a connected node
- **AI Assist** — On an empty canvas, click "Create with AI" to generate a flow from a description

### Drawing Connections

Click an output port on one node and drag to an input port on another to create a connection. Connections define execution order and data flow between steps.

### Context Menu

Right-click the canvas to access:
- **Auto Layout** — Automatically arrange nodes using ELK layout algorithm
- **Reset Zoom** — Return to default zoom level

## Node Types

| Type | Icon | Description |
|------|------|-------------|
| **Start** | ▶ | Flow entry point (automatically created) |
| **End** | ⏹ | Flow exit point (automatically created) |
| **Input** | ✏️ | Collect user input during execution |
| **LLM** | 🧠 | Invoke the AI agent with a prompt |
| **Tool** | 🔧 | Execute a specific tool directly |
| **Output** | 💬 | Display a result to the user |
| **Update State** | ⚙️ | Modify flow state variables |

The toolbar shows the addable types: Input, LLM, Tool, State, and Output. Start and End nodes are created automatically with new flows.

## AI Assist

AI Assist helps generate and modify flow nodes using natural language:

- **Create with AI** — Available on empty canvases, generates an entire flow from a description
- **Node AI Assist** — When editing a node, click "AI Assist" to get help configuring it
- **Multi-node Assist** — Select multiple nodes, then click the "AI Assist" button that appears to modify them together

AI Assist opens a chat panel where you describe what you need. The AI generates or modifies nodes and connections on the canvas.

## Running a Flow

Click the **Run** button in the header to execute the flow. The editor switches to execution mode:

- The canvas becomes read-only and a chat panel opens
- Click **Start Execution** to begin
- Active nodes highlight as they execute
- Output appears in the chat panel
- Input nodes pause execution and prompt for user input
- Errors display inline with details

Use the **Stop** button to abort execution, or **Start Again** to re-run after completion. Toggle **Auto-Approve** to skip tool call confirmations.

## YAML Representation

Flows have a bidirectional YAML representation. You can:

- Export flows as YAML for version control
- Edit YAML directly and see changes reflected on the canvas
- Share flows as YAML files

## Saving

Flows are saved automatically as you edit. They persist server-side and are accessible from the Flows tab sidebar.
