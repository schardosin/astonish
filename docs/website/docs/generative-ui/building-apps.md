# Building Apps

This guide walks through creating, refining, and saving Generative UI apps in Astonish Studio.

## Step 1: Describe What You Want

Start by telling the agent what you need. Be specific about layout, data, and interactions:

```
You:    "Create a time tracker where I can start/stop a timer for different
         projects, see today's logged hours in a pie chart, and have a
         weekly summary table"
Agent:  [generating app] Building a time tracker with timer controls,
        pie chart breakdown, and weekly summary...

        [renders interactive time tracker]

        Here's your time tracker. Click a project to start timing.
        The pie chart updates in real time.
```

## Step 2: Refine Iteratively

Follow up with adjustments. The agent preserves existing functionality while applying changes:

```
You:    "Add a dark mode toggle and make the project colors customizable"
Agent:  [updating app] Adding dark mode and a color picker for each project...

        [re-renders with new features]

You:    "The weekly table should show hours per project as columns,
         not rows. And add export to CSV."
Agent:  [updating app] Pivoting the table layout and adding CSV export...
```

Each refinement is instant — Sucrase recompiles in milliseconds and the iframe updates without a full page reload.

## Step 3: Save as a Named App

Once satisfied, save the app:

```
You:    "Save this as 'Time Tracker'"
Agent:  [saved] App "Time Tracker" is now available in your Apps tab.
        It will persist your timer data between sessions.
```

Saved apps appear in the Studio sidebar under the Apps tab and retain their state across sessions.

## What Works Well

Generative UI excels at certain categories of applications:

**Dashboards & Monitors**
- Sprint velocity trackers
- System health dashboards
- Sales pipeline visualizations
- Budget burn-down charts

**Productivity Tools**
- Time trackers, habit trackers
- Kanban boards, priority matrices
- Meeting note templates with timers

**Calculators & Converters**
- Pricing calculators
- Unit converters
- Compound interest projectors
- Scoring rubrics

**Data Exploration**
- CSV/JSON viewers with filtering
- API response explorers
- Log analyzers with search

## Tips for Good Prompts

**Be specific about layout:**
> "Two-column layout: left side has the form, right side shows a live preview"

**Mention data sources early:**
> "Pull ticket counts from our Jira MCP server and show them as a stacked bar chart"

**Describe interactions explicitly:**
> "Clicking a row expands it to show details. Double-click to edit inline."

**Reference familiar patterns:**
> "Like a Trello board but with swimlanes grouped by priority instead of status"

**State what you don't want:**
> "No animations. Keep it minimal — just the data, no decorative elements."

## Limitations

- Apps run client-side in a sandboxed iframe — no direct filesystem or database access.
- Network requests must go through [data hooks](./data-hooks.md) (no raw `fetch` calls).
- Maximum component size is ~500 lines of JSX. For larger apps, break into multiple components.
- Server-side rendering is not supported — apps are fully client-rendered.

## Next Steps

- [Data Hooks](./data-hooks.md) — Connect your app to live data sources
- [Sharing & Persistence](./sharing.md) — Publish apps to your team
