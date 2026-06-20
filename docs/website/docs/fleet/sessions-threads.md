# Sessions & Threads

Fleet agents communicate through structured sessions and threads. This system ensures message isolation between agent pairs and enables organized, auditable conversations.

## Pairwise Sessions

Each hub–spoke relationship has its own dedicated session. In a fleet with one hub and three spokes, there are exactly three sessions:

```
Hub ←→ Backend   (Session A)
Hub ←→ Frontend  (Session B)
Hub ←→ DevOps    (Session C)
```

Sessions are long-lived — they persist for the entire duration of a [plan](./plans.md). All communication between a specific hub–spoke pair flows through their shared session.

## Message Routing

The hub agent's messages are routed to the correct spoke based on addressing:

1. Hub composes a message directed at a spoke (e.g., "backend")
2. The fleet router delivers it to the hub–backend session
3. The backend spoke processes the message and responds
4. The response is delivered back to the hub within the same session

Spokes never receive messages from other spokes. All coordination passes through the hub, maintaining a clear chain of command.

## Thread Isolation

Within a session, individual tasks are organized into threads:

```
Session: Hub ←→ Backend
├── Thread 1: "Design auth schema"
│   ├── Hub: Please design the database schema for...
│   ├── Backend: Here's the proposed schema...
│   └── Hub: Approved. Proceed with implementation.
├── Thread 2: "Implement session middleware"
│   ├── Hub: Now implement the session middleware...
│   └── Backend: Done. The middleware handles...
```

Threads provide:
- **Context isolation** — Each task gets a clean conversation without prior task context polluting it
- **Parallel work** — Multiple threads in a session can be active simultaneously
- **Auditability** — Every decision and exchange is traceable to a specific task

## Viewing Sessions

In [Studio](./studio.md), the Fleet tab shows all active sessions and their threads. You can inspect any message exchange between agents.

From the CLI:

```bash
# List sessions for a plan
astonish fleet sessions --plan <plan-id>

# View messages in a session
astonish fleet session show <session-id>
```
