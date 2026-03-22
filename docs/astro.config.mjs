// @ts-check
import { defineConfig } from 'astro/config';
import starlight from '@astrojs/starlight';
import tailwindcss from '@tailwindcss/vite';
import six from '@six-tech/starlight-theme-six';
import mermaid from 'astro-mermaid';

// https://astro.build/config
export default defineConfig({
  site: 'https://schardosin.github.io',
  base: '/astonish/',
  
  integrations: [
    mermaid({
      autoTheme: true,
    }),
    starlight({
      plugins: [six({})],
      title: 'Astonish',
      description: 'A personal AI assistant platform that learns, remembers, and automates',
      logo: {
        src: './src/assets/logo.svg',
        replacesTitle: false,
      },
      social: [
        { icon: 'github', label: 'GitHub', href: 'https://github.com/schardosin/astonish' }
      ],
      customCss: [
        './src/styles/custom.css',
      ],
      sidebar: [
        {
          label: 'Getting Started',
          items: [
            { label: 'Introduction', slug: 'getting-started/introduction' },
            { label: 'Installation', slug: 'getting-started/installation' },
            { label: 'Quick Setup', slug: 'getting-started/quick-setup' },
            { label: 'Running as a Service', slug: 'getting-started/running-as-a-service' },
            { label: 'Choose Your Interface', slug: 'getting-started/choose-your-interface' },
          ],
        },
        {
          label: 'Chat & Sessions',
          collapsed: true,
          items: [
            { label: 'Chat Overview', slug: 'chat/overview' },
            { label: 'Sessions', slug: 'chat/sessions' },
            { label: 'Memory & Knowledge', slug: 'chat/memory' },
            { label: 'Skills', slug: 'chat/skills' },
            { label: 'Sub-agents & Delegation', slug: 'chat/sub-agents' },
          ],
        },
        {
          label: 'Built-in Tools',
          collapsed: true,
          items: [
            { label: 'Tools Overview', slug: 'tools/overview' },
            { label: 'File & Search', slug: 'tools/file-search' },
            { label: 'Shell & Process', slug: 'tools/shell-process' },
            { label: 'Web & HTTP', slug: 'tools/web-http' },
            { label: 'Browser Automation', slug: 'tools/browser' },
            { label: 'Email Tools', slug: 'tools/email' },
            { label: 'Credential Management', slug: 'tools/credentials' },
            { label: 'Scheduler & Agent', slug: 'tools/scheduler-agent' },
          ],
        },
        {
          label: 'Studio',
          collapsed: true,
          items: [
            { label: 'Overview', slug: 'studio' },
            { label: 'Studio Chat', slug: 'studio/chat' },
            { label: 'Flow Editor', slug: 'studio/flow-editor' },
            { label: 'Running & Debugging', slug: 'studio/running-debugging' },
            { label: 'Settings', slug: 'studio/settings' },
            { label: 'Keyboard Shortcuts', slug: 'studio/keyboard-shortcuts' },
          ],
        },
        {
          label: 'CLI Reference',
          collapsed: true,
          items: [
            { label: 'Overview', slug: 'cli' },
            { label: 'chat & sessions', slug: 'cli/chat-sessions' },
            { label: 'flows', slug: 'cli/flows' },
            { label: 'daemon & scheduler', slug: 'cli/daemon-scheduler' },
            { label: 'channels & fleet', slug: 'cli/channels-fleet' },
            { label: 'Utility Commands', slug: 'cli/utility-commands' },
          ],
        },
        {
          label: 'Communication Channels',
          collapsed: true,
          items: [
            { label: 'Overview', slug: 'channels/overview' },
            { label: 'Telegram', slug: 'channels/telegram' },
            { label: 'Email Channel', slug: 'channels/email-channel' },
          ],
        },
        {
          label: 'Fleet — Multi-Agent Teams',
          collapsed: true,
          items: [
            { label: 'Overview', slug: 'fleet/overview' },
            { label: 'Templates', slug: 'fleet/templates' },
            { label: 'Plans', slug: 'fleet/plans' },
            { label: 'Sessions & Threads', slug: 'fleet/sessions-threads' },
            { label: 'Fleet in Studio', slug: 'fleet/studio-fleet' },
          ],
        },
        {
          label: 'Flows & Automation',
          collapsed: true,
          items: [
            { label: 'Overview', slug: 'flows/overview' },
            { label: 'YAML Reference', slug: 'flows/yaml-reference' },
            { label: 'Flow Distillation', slug: 'flows/distillation' },
            { label: 'Nodes, Edges & State', slug: 'flows/nodes-edges-state' },
          ],
        },
        {
          label: 'Configuration',
          collapsed: true,
          items: [
            { label: 'AI Providers', slug: 'configuration/providers' },
            { label: 'MCP Servers', slug: 'configuration/mcp-servers' },
            { label: 'Taps & Flow Store', slug: 'configuration/taps' },
            { label: 'Config File Reference', slug: 'configuration/config-reference' },
            { label: 'Authentication', slug: 'configuration/authentication' },
          ],
        },
        {
          label: 'Reference',
          collapsed: true,
          items: [
            { label: 'Glossary', slug: 'reference/glossary' },
            { label: 'Troubleshooting', slug: 'reference/troubleshooting' },
          ],
        },
      ],
      head: [
        {
          tag: 'meta',
          attrs: {
            property: 'og:image',
            content: '/astonish/og-image.png',
          },
        },
      ],
    }),
  ],

  vite: {
    plugins: [tailwindcss()],
  },
});
