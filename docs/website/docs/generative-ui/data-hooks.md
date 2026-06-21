# Data Hooks

Data hooks connect Generative UI apps to live data sources, AI capabilities, and persistent storage. All hooks are available globally in any generated app — no imports needed (though you can import from `'astonish'` for clarity).

## Overview

| Hook | Purpose | Returns |
|------|---------|---------|
| `useAppData(sourceId)` | Fetch data from APIs and MCP tools | `< data, loading, error, refetch >` |
| `useAppAction(actionId)` | Trigger mutations/actions | `async (payload) => result` |
| `useAppAI(options)` | Run LLM calls from the app | `async (prompt, callOptions?) => string` |
| `useAppState()` | Per-app persistent SQLite database | `< exec(sql, params), query(sql, params) >` |

All data fetching is **backend-proxied** — credentials, API keys, and OAuth tokens are stored server-side and never exposed to the iframe sandbox.

## useAppData

Fetches data from external sources via a string `sourceId`. The format determines the data source type.

### HTTP API

Fetch from any HTTP endpoint:

```jsx
function GitHubStats() {
  const < data, loading, error > = useAppData(
    'http:GET:https://api.github.com/repos/myorg/myrepo/stats/contributors'
  );

  if (loading) return <div>Loading...</div>;
  if (error) return <div>Error: <error.message></div>;

  return (
    <div>
      {data.slice(0, 5).map((contributor) => (
        <div key=<contributor.author.login>>
          <contributor.author.login>: <contributor.total> commits
        </div>
      ))}
    </div>
  );
}
```

### HTTP with Credential Authentication

Append `@credential-name` to authenticate using a stored credential:

```jsx
function PrivateRepos() {
  const < data, loading > = useAppData(
    'http:GET:https://api.github.com/user/repos@github-token'
  );

  // credential is resolved server-side, never exposed to the browser
}
```

### MCP Tool Call

Connect to any configured MCP server tool:

```jsx
function TicketDashboard() {
  const < data, loading, error > = useAppData(
    'mcp:jira/list_tickets'
  );

  if (loading) return <div>Loading tickets...</div>;
  if (error) return <div>Error: <error.message></div>;

  return (
    <ul>
      {data.tickets.map((t) => (
        <li key=<t.id>><t.title> — <t.assignee></li>
      ))}
    </ul>
  );
}
```

The sourceId format for MCP is `mcp:<server>/<tool>`.

### Dynamic URLs

For dynamic data, construct the sourceId with variables:

```jsx
function UserProfile(< userId >) {
  const < data, loading > = useAppData(
    'http:GET:https://api.example.com/users/' + encodeURIComponent(userId)
  );
  // ...
}
```

## useAppAction

Triggers mutations or write operations. Returns an async function:

```jsx
function CreateTicket() {
  const createTicket = useAppAction('mcp:jira/create_ticket');
  const [title, setTitle] = useState('');

  const handleSubmit = async () => {
    const result = await createTicket(< title, priority: 'medium' >);
    console.log('Created:', result.id);
  };

  return (
    <div>
      <input value=<title> onChange=<(e) => setTitle(e.target.value)> />
      <button onClick=<handleSubmit>>Create Ticket</button>
    </div>
  );
}
```

## useAppAI

Runs one-shot LLM calls from within the app. Useful for summarization, classification, extraction, or any text transformation.

```jsx
function FeedbackAnalyzer() {
  const askAI = useAppAI(< system: 'You are a sentiment analysis expert.' >);
  const [feedback, setFeedback] = useState('');
  const [result, setResult] = useState(null);
  const [loading, setLoading] = useState(false);

  const analyze = async () => {
    setLoading(true);
    const text = await askAI(
      `Classify this feedback as positive, negative, or neutral. Extract key topics. Feedback: "$<feedback>"`,
      < context: feedback >
    );
    setResult(text);
    setLoading(false);
  };

  return (
    <div>
      <textarea
        value=<feedback>
        onChange=<(e) => setFeedback(e.target.value)>
        placeholder="Paste customer feedback..."
      />
      <button onClick=<analyze> disabled=<loading>>
        <loading ? 'Analyzing...' : 'Analyze'>
      </button>
      {result && <p><result></p>}
    </div>
  );
}
```

The `useAppAI` hook accepts an options object with a `system` prompt. It returns an async function that takes a user prompt and optional call options (like `< context >` for additional context).

## useAppState

Provides a reactive SQLite database scoped per-user, per-app. State survives across sessions and browser refreshes.

```jsx
function HabitTracker() {
  const db = useAppState();

  // Create table on first load
  React.useEffect(() => <
    db.exec(`CREATE TABLE IF NOT EXISTS habits (
      id INTEGER PRIMARY KEY AUTOINCREMENT,
      name TEXT NOT NULL,
      created_at DATETIME DEFAULT CURRENT_TIMESTAMP
    )`);
    db.exec(`CREATE TABLE IF NOT EXISTS completions (
      habit_id INTEGER,
      date TEXT,
      PRIMARY KEY (habit_id, date)
    )`);
  >, []);

  // Reactive query — auto-refreshes when data changes
  const habits = db.query('SELECT * FROM habits ORDER BY created_at DESC');
  const today = new Date().toISOString().split('T')[0];
  const todayCompletions = db.query(
    'SELECT habit_id FROM completions WHERE date = ?', [today]
  );

  const addHabit = (name) => <
    db.exec('INSERT INTO habits (name) VALUES (?)', [name]);
  >;

  const toggleToday = (habitId) => {
    const completed = todayCompletions.data?.some(c => c.habit_id === habitId);
    if (completed) <
      db.exec('DELETE FROM completions WHERE habit_id = ? AND date = ?', [habitId, today]);
    > else <
      db.exec('INSERT INTO completions (habit_id, date) VALUES (?, ?)', [habitId, today]);
    >
  };

  if (habits.loading) return <div>Loading...</div>;

  return (
    <div>
      {habits.data?.map((h) => (
        <div key=<h.id> onClick=<() => toggleToday(h.id)>>
          <h.name>
        </div>
      ))}
    </div>
  );
}
```

Key features:
- `db.exec(sql, params)` — Execute write/DDL statements
- `db.query(sql, params)` — Reactive read queries (auto-refresh on mutations)
- Results from `db.query()` have `.data`, `.loading`, `.error` properties
- State is stored per-user, per-app — even shared apps have independent state per user

## Combining Hooks

Hooks compose naturally for data-driven interactive apps:

```jsx
function SmartInventory() {
  const < data: inventory > = useAppData('mcp:warehouse/get_inventory');
  const db = useAppState();
  const askAI = useAppAI(< system: 'You are an inventory analyst.' >);

  const analyzeStock = async () => {
    const analysis = await askAI(
      `Identify items likely to run out within 7 days`,
      < context: JSON.stringify(inventory) >
    );
    db.exec('INSERT INTO analyses (result, created_at) VALUES (?, datetime("now"))', [analysis]);
  };

  // Combine all hooks for a reactive dashboard
}
```

## Security

- All network requests are proxied through the Astonish backend
- Credentials referenced via `@credential-name` are resolved server-side
- The iframe sandbox prevents direct network access (`fetch` is blocked)
- App code runs in a restricted Content Security Policy

## Next Steps

- [Building Apps](./building-apps.md) — Creating and refining apps
- [Sharing & Persistence](./sharing.md) — Publishing and state isolation
