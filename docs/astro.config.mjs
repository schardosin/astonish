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
      description: 'Build Production AI Agents in Minutes, Not Months',
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
            { label: 'Choose Your Path', slug: 'getting-started/choose-your-path' },
            {
              label: 'Quickstart',
              collapsed: false,
              items: [
                { label: 'Studio Quickstart', slug: 'getting-started/quickstart/studio' },
                { label: 'CLI Quickstart', slug: 'getting-started/quickstart/cli' },
              ],
            },
          ],
        },
        {
          label: 'Using Astonish Studio',
          collapsed: true,
          items: [
            { label: 'Overview', slug: 'studio' },
            { label: 'Creating Flows', slug: 'studio/creating-flows' },
            { label: 'Working with Nodes', slug: 'studio/working-with-nodes' },
            { label: 'Connecting Edges', slug: 'studio/connecting-edges' },
            { label: 'Running & Debugging', slug: 'studio/running-debugging' },
            { label: 'Keyboard Shortcuts', slug: 'studio/keyboard-shortcuts' },
            { label: 'Exporting & Sharing', slug: 'studio/exporting-sharing' },
          ],
        },
        {
          label: 'Using the CLI',
          collapsed: true,
          items: [
            { label: 'Overview', slug: 'cli' },
            { label: 'Running Flows', slug: 'cli/running-agents' },
            { label: 'Managing Flows', slug: 'cli/managing-flows' },
            { label: 'Parameters & Variables', slug: 'cli/parameters' },
            { label: 'Automation', slug: 'cli/automation' },
          ],
        },
        {
          label: 'Using the App',
          collapsed: true,
          items: [
            { label: 'Configure Providers', slug: 'using-the-app/configure-providers' },
            { label: 'Add MCP Servers', slug: 'using-the-app/add-mcp-servers' },
            { label: 'Manage Tap Repositories', slug: 'using-the-app/manage-taps' },
            { label: 'Share Your Flows', slug: 'using-the-app/share-flows' },
            { label: 'Troubleshooting', slug: 'using-the-app/troubleshooting' },
          ],
        },
        {
          label: 'Key Concepts',
          collapsed: true,
          items: [
            { label: 'Flows', slug: 'concepts/flows' },
            { label: 'Nodes', slug: 'concepts/nodes' },
            { label: 'State', slug: 'concepts/state' },
            { label: 'MCP & Tools', slug: 'concepts/mcp' },
            { label: 'Taps', slug: 'concepts/taps' },
            { label: 'YAML Reference', slug: 'concepts/yaml' },
          ],
        },
        {
          label: 'Reference',
          collapsed: true,
          items: [
            { label: 'CLI Commands', slug: 'reference/cli-commands' },
            { label: 'Glossary', slug: 'reference/glossary' },
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