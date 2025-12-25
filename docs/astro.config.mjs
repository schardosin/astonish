// @ts-check
import { defineConfig } from 'astro/config';
import starlight from '@astrojs/starlight';
import tailwindcss from '@tailwindcss/vite';

// https://astro.build/config
export default defineConfig({
  site: 'https://schardosin.github.io',
  base: '/astonish/',
  
  integrations: [
    starlight({
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
            { label: 'Quick Start', slug: 'getting-started/quick-start' },
          ],
        },
        {
          label: 'Concepts',
          items: [
            { label: 'Agent Flows', slug: 'concepts/flows' },
            { label: 'YAML Configuration', slug: 'concepts/yaml' },
            { label: 'Nodes & Edges', slug: 'concepts/nodes' },
            { label: 'MCP Integration', slug: 'concepts/mcp' },
            { label: 'Flow Store & Taps', slug: 'concepts/taps' },
          ],
        },
        {
          label: 'Commands',
          items: [
            { label: 'astonish studio', slug: 'commands/studio' },
            { label: 'astonish agents', slug: 'commands/agents' },
            { label: 'astonish tools', slug: 'commands/tools' },
            { label: 'astonish tap', slug: 'commands/tap' },
            { label: 'astonish flows', slug: 'commands/flows' },
          ],
        },
        {
          label: 'Tutorials',
          items: [
            { label: 'Your First Agent', slug: 'tutorials/first-agent' },
            { label: 'PR Description Generator', slug: 'tutorials/pr-generator' },
            { label: 'Using MCP Tools', slug: 'tutorials/mcp-tools' },
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