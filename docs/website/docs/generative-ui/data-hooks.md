# Data Hooks

Data hooks connect Generative UI apps to live data sources, AI capabilities, and persistent storage. All three hooks are available globally in any generated app — no imports needed.

## Overview

| Hook | Purpose | Backed By |
|------|---------|-----------|
| `useAppData` | Fetch data from APIs and tools | MCP servers, REST/OAuth, static config |
| `useAppAI` | Run LLM calls from the app | Configured AI provider |
| `useAppState` | Persist key-value data | Per-user, per-app database storage |

All data fetching is **backend-proxied** — credentials, API keys, and OAuth tokens are stored server-side and never exposed to the iframe sandbox.

## useAppData

Fetches data from external sources. Supports three modes:

### MCP Tool Call

Connect to any configured MCP server tool:

```jsx
function TicketDashboard() {
  const { data, loading, error, refetch } = useAppData({
    source: "mcp",
    server: "jira",
    tool: "list_tickets",
    args: {
      project: "ENG",
      status: "open",
    },
    pollInterval: 30000, // Refresh every 30s
  });

  if (loading) return <div>Loading tickets...</div>;
  if (error) return <div>Error: {error.message}</div>;

  return (
    <ul>
      {data.tickets.map((t) => (
        <li key={t.id}>{t.title} — {t.assignee}</li>
      ))}
    </ul>
  );
}
```

### REST API with OAuth

Call any REST endpoint using stored OAuth credentials:

```jsx
function GitHubStats() {
  const { data, loading } = useAppData({
    source: "rest",
    url: "https://api.github.com/repos/myorg/myrepo/stats/contributors",
    credential: "github-oauth",    // References stored credential
    method: "GET",
    headers: {
      "Accept": "application/vnd.github.v3+json",
    },
    transform: (raw) => raw.slice(0, 5), // Client-side transform
  });

  if (loading) return <div>Loading...</div>;

  return (
    <div>
      {data.map((contributor) => (
        <div key={contributor.author.login}>
          {contributor.author.login}: {contributor.total} commits
        </div>
      ))}
    </div>
  );
}
```

### Static Configuration

Provide data directly for prototyping or configuration-driven apps:

```jsx
function PricingTable() {
  const { data } = useAppData({
    source: "static",
    value: {
      plans: [
        { name: "Starter", price: 0, features: ["5 flows", "1 agent"] },
        { name: "Pro", price: 29, features: ["Unlimited flows", "10 agents", "Priority support"] },
        { name: "Enterprise", price: null, features: ["Custom", "SSO", "SLA"] },
      ],
    },
  });

  return <PricingGrid plans={data.plans} />;
}
```

## useAppAI

Runs one-shot LLM calls from within the app. Useful for summarization, classification, extraction, or any text transformation.

```jsx
function FeedbackAnalyzer() {
  const [feedback, setFeedback] = useState("");
  const { result, loading, run } = useAppAI({
    prompt: `Classify this customer feedback into: positive, negative, neutral.
             Then extract the key topics mentioned.
             Feedback: "${feedback}"`,
    schema: {
      sentiment: "string",
      topics: "string[]",
      summary: "string",
    },
  });

  return (
    <div>
      <textarea
        value={feedback}
        onChange={(e) => setFeedback(e.target.value)}
        placeholder="Paste customer feedback..."
      />
      <button onClick={run} disabled={loading}>
        {loading ? "Analyzing..." : "Analyze"}
      </button>
      {result && (
        <div>
          <p>Sentiment: {result.sentiment}</p>
          <p>Topics: {result.topics.join(", ")}</p>
          <p>Summary: {result.summary}</p>
        </div>
      )}
    </div>
  );
}
```

The `schema` field provides structured output — the LLM response is parsed and validated against the declared shape. The `run` function is manually triggered (not automatic) to control when LLM calls happen.

## useAppState

Persists key-value data per user, per app. State survives across sessions and browser refreshes.

```jsx
function HabitTracker() {
  const { state, setState, loading } = useAppState({
    key: "habits",
    defaultValue: {
      habits: [],
      completions: {},
    },
  });

  const addHabit = (name) => {
    setState({
      ...state,
      habits: [...state.habits, { id: Date.now(), name }],
    });
  };

  const toggleToday = (habitId) => {
    const today = new Date().toISOString().split("T")[0];
    const key = `${habitId}_${today}`;
    setState({
      ...state,
      completions: {
        ...state.completions,
        [key]: !state.completions[key],
      },
    });
  };

  if (loading) return <div>Loading...</div>;

  return (
    <div>
      {state.habits.map((h) => (
        <div key={h.id} onClick={() => toggleToday(h.id)}>
          {h.name}
        </div>
      ))}
    </div>
  );
}
```

State is stored in the Astonish database and scoped to the individual user — even when an app is shared with a team, each user has their own independent state.

## Combining Hooks

Hooks compose naturally for data-driven interactive apps:

```jsx
function SmartInventory() {
  const { data: inventory } = useAppData({
    source: "mcp",
    server: "warehouse",
    tool: "get_inventory",
    pollInterval: 60000,
  });

  const { state, setState } = useAppState({
    key: "alerts",
    defaultValue: { thresholds: {} },
  });

  const { run: analyzeStock } = useAppAI({
    prompt: `Given this inventory data, identify items likely to run
             out within 7 days: ${JSON.stringify(inventory)}`,
    schema: { atRisk: "object[]" },
  });

  // Combine all three for a reactive dashboard
}
```

## Next Steps

- [Building Apps](./building-apps.md) — Creating and refining apps
- [Sharing & Persistence](./sharing.md) — Publishing and state isolation
