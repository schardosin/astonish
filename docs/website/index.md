---
layout: home

title: Astonish
titleTemplate: false

hero:
  name: Astonish
  text: AI Agent Platform That Makes Your Whole Team Smarter
  tagline: When one person solves a problem, everyone benefits. Multi-tenant. Three-tier memory. Enterprise-ready. Built in Go.
  actions:
    - theme: brand
      text: Get Started
      link: /docs/
    - theme: alt
      text: View on GitHub
      link: https://github.com/schardosin/astonish
  image:
    src: /logo_icon_background.svg
    alt: Astonish

features:
  - icon:
      src: /highlights/memory.svg
    title: Three-Tier Memory
    details: Personal, team, and org-level knowledge searched together with intelligent weighting. When someone solves a problem, the solution flows to everyone who needs it — automatically.
    link: /docs/
  - icon:
      src: /highlights/distillation.svg
    title: Flow Distillation
    details: Solve problems interactively, then distill into reusable YAML flows. Share with the team, schedule with cron, version control in PRs — chat becomes infrastructure.
    link: /docs/
  - icon:
      src: /highlights/generative-ui.svg
    title: Generative UI
    details: Describe any dashboard or tool in plain English and get a live React app. Persistent state, MCP data connections, embedded AI calls — no frontend setup required.
    link: /docs/
  - icon:
      src: /highlights/fleet.svg
    title: Multi-Tenant Platform
    details: Organizations, teams, and personal workspaces with cascading defaults. Role-based access, publish/fork resources. Same binary — scales from local to enterprise.
    link: /docs/
  - icon:
      src: /highlights/security.svg
    title: Enterprise Security
    details: Envelope encryption, per-org sandboxes, OIDC/SSO, immutable audit logs, and database-level isolation. Built for organizations that take security seriously.
    link: /docs/
  - icon:
      src: /highlights/channels.svg
    title: Multi-Channel Access
    details: Telegram, Email, Slack, Remote CLI, and Studio — all connected to the same platform. Switch context between orgs and teams on the fly from any channel.
    link: /docs/
---

<script setup>
import ThemedTeamMembers from '@components/ThemedTeamMembers.vue'

const platforms = [
  {
    name: 'Linux',
    logo: '/platforms/linux.svg'
  },
  {
    name: 'macOS',
    logo: '/platforms/macos.svg',
    darkLogo: '/platforms/macos-dark.svg'
  },
  {
    name: 'Windows',
    logo: '/platforms/windows.svg'
  },
  {
    name: 'Kubernetes',
    logo: '/platforms/kubernetes.svg'
  },
  {
    name: 'Docker',
    logo: '/platforms/docker.svg'
  },
  {
    name: 'OpenShell',
    logo: '/platforms/nvidia.svg'
  },
]
</script>

## Runs Anywhere You Deploy

**Deploy on any infrastructure — from your laptop to a Kubernetes cluster**

<ThemedTeamMembers size="small" :members="platforms" />

---

## From Local to Cloud

**Run locally with SQLite for instant setup, or deploy with PostgreSQL for your entire organization.**

58+ built-in tools, 15+ AI providers, MCP native, sub-agent delegation — the same platform engine powers every deployment. Knowledge compounds across every conversation, every team member, every day.
