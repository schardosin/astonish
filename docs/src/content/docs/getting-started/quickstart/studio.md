---
title: Studio Quickstart
description: Build your first AI agent using the visual editor in under 5 minutes
sidebar:
  order: 1
---

# Studio Quickstart

This guide walks you through creating and running your first AI agent using Astonish Studio—the visual flow editor.

**Time:** ~5 minutes

## Prerequisites

- [Astonish installed](/astonish/getting-started/installation/)
- An API key from any supported provider (we'll set this up together)

## 1. Launch Studio

Open your terminal and run:

```bash
astonish studio
```

This starts the visual editor at **http://localhost:9393**. Open it in your browser.

![Studio Launch](/astonish/images/wizard-welcome.webp)
*Astonish Studio welcome screen*

## 2. Configure Your Provider

On first launch, you'll see the **Setup Wizard**. This guides you through connecting an AI provider.

### Supported Providers

| Provider | Type | What You Need |
|----------|------|---------------|
| OpenRouter | Cloud | API Key (recommended for beginners) |
| Google Gemini | Cloud | API Key |
| Anthropic Claude | Cloud | API Key |
| OpenAI | Cloud | API Key |
| Groq | Cloud | API Key |
| Ollama | Local | Just install Ollama |
| LM Studio | Local | Just install LM Studio |

### Setup Steps

1. Select your provider from the list
2. Enter your API key (or leave blank for local providers)
3. Choose a default model
4. Click **Complete Setup**

:::tip[First Time?]
We recommend **OpenRouter** for beginners—it gives you access to multiple models with one API key. [Get a free key at openrouter.ai](https://openrouter.ai/)
:::

## 3. Create a New Flow

1. Click **+ New Flow** in the sidebar (or press `N`)
2. Enter a name: `hello world`
3. Click **Create**

You'll see an empty canvas with **START** and **END** nodes.

![Empty Canvas](/astonish/images/introduction-empty_canvas.webp)
*A new flow with START and END nodes*

## 4. Add an LLM Node

1. Click the **+** button on the **START** node
2. Select **LLM** from the node type menu
3. A new LLM node appears, already connected to START

### Configure the Prompt

Click the node to open the editor panel:

1. **Name:** `greet`
2. **Prompt:** `Greet the user warmly and ask how you can help them today.`

### Configure the Output

Switch to the **Output** tab:

1. The **Output Model** already has `response` — this saves the AI's reply to state
2. Under **User Message**, click **+ Add**
3. Enter `response` — this displays the saved reply to the user
4. Click **Done** to close the panel

![LLM Output Configuration](/astonish/images/introduction-flow_node_user_message.webp)
*Configure the Output tab to display the response to the user*

## 5. Connect to END

1. Click the **+** button on the `greet` node
2. Select **End** from the node type menu

Your flow is now complete: **START → greet → END**

![Complete Flow with Node Configuration](/astonish/images/introduction-flow_node_dialog.webp)
*The complete flow with the LLM node configured*

## 6. Run Your Flow

1. Click the **▶ Run** button in the top bar
2. Watch the execution in the chat panel on the right
3. The AI will greet you!

![Flow Execution](/astonish/images/introduction-flow_run.webp)
*Seeing your flow run in real-time*

## What You Built

Your flow is automatically saved as a YAML file at `~/.astonish/agents/hello_world.yaml`.

Here's the YAML that Studio generated:

```yaml
description: hello world
nodes:
  - name: greet
    type: llm
    system: You are a helpful assistant.
    prompt: Greet the user warmly and ask how you can help them today
    output_model:
      response: str
    user_message:
      - response
flow:
  - from: START
    to: greet
  - from: greet
    to: END
```

You can now run this same flow from the command line:

```bash
astonish flows run hello_world
```

## Next Steps

Now that you've built your first flow:

- **[Working with Nodes](/astonish/studio/working-with-nodes/)** — Learn all node types
- **[Connecting Edges](/astonish/studio/connecting-edges/)** — Add conditions and branches
- **[Configure Providers](/astonish/using-the-app/configure-providers/)** — Add more AI providers
- **[Add MCP Servers](/astonish/using-the-app/add-mcp-servers/)** — Connect tools like GitHub, Slack
