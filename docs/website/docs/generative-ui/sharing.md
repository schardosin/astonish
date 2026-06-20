# Sharing & Persistence

Generative UI apps in Astonish are designed for collaboration. Apps start as personal creations and can be progressively shared with your team and organization.

## App Visibility Levels

| Level | Who Can See | Who Can Edit |
|-------|-------------|--------------|
| Personal | Only you | Only you |
| Team | Team members | Only you (others can fork) |
| Organization | All org members | Only you (others can fork) |

## Personal Apps

Every app starts as personal. It appears in your Apps tab in Studio and is invisible to others. Your app state (via `useAppState`) is fully private.

```
You:    "Save this as 'My Sprint Board'"
Agent:  [saved] App "My Sprint Board" saved to your personal apps.
```

## Publishing to Team

Share an app with your team so they can use it and build on it:

```
You:    "Publish 'Sprint Board' to the engineering team"
Agent:  [published] "Sprint Board" is now visible to the Engineering team.
        Team members will see it in their Apps tab and can fork their
        own copy to customize.
```

Published apps are read-only for team members. The original author retains edit control. Team members interact with the live app and have their own independent state.

## Forking

Any team or org app can be forked to create an independent personal copy:

```
You:    "Fork the team's Sprint Board and rename it 'My Board'"
Agent:  [forked] Created "My Board" from "Sprint Board".
        This is your personal copy — edits won't affect the original.
```

Forks are fully independent. Changes to the original do not propagate to forks, and vice versa.

## Promoting to Organization

Team apps can be promoted to organization-wide visibility by admins:

```
You:    "Promote 'Sprint Board' to the organization"
Agent:  [promoted] "Sprint Board" is now available to all organization members.
```

This is useful for standardized tools — onboarding dashboards, incident response panels, or company-wide metric trackers.

## State Isolation

A critical design principle: **app definitions are shared, but state is always personal**.

When a team uses the same published app:
- Everyone sees the same UI (same component code).
- Everyone has their own `useAppState` data — Alice's tracked habits don't appear in Bob's view.
- `useAppData` results depend on the user's own connected credentials and permissions.

This means a single shared "Time Tracker" app gives each team member their own private time log, while the team lead could build a separate "Team Hours" app that aggregates via API.

## The Apps Tab in Studio

The Studio sidebar organizes apps into sections:

- **Recent** — Last 5 apps you interacted with.
- **Personal** — All your private apps.
- **Team** — Apps published by team members.
- **Organization** — Org-wide apps (if applicable).

Each app entry shows its name, a description, and a last-modified timestamp. Click to launch; long-press or right-click for options (rename, publish, fork, delete).

## Deleting Apps

Personal apps can be deleted at any time. Published apps can only be unpublished by their author — this removes team/org visibility but retains the personal copy.

```
You:    "Delete my Time Tracker app"
Agent:  [deleted] "Time Tracker" has been removed. Any persisted state
        has been cleared.
```

## Next Steps

- [Generative UI Overview](./index.md) — How Generative UI works
- [Data Hooks](./data-hooks.md) — Connecting apps to live data
- [Building Apps](./building-apps.md) — Creating apps step by step
