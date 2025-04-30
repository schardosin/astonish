import type {SidebarsConfig} from '@docusaurus/plugin-content-docs';

// This runs in Node.js - Don't use client-side code here (browser APIs, JSX...)

/**
 * Creating a sidebar enables you to:
 - create an ordered group of docs
 - render a sidebar for each doc of that group
 - provide next/previous navigation

 The sidebars can be generated from the filesystem, or explicitly defined here.

 Create as many sidebars as you want.
 */
const sidebars: SidebarsConfig = {
  tutorialSidebar: [
    'intro',
    {
      type: 'category',
      label: 'Getting Started',
      items: [
        'getting-started/installation',
        'getting-started/configuration',
        'getting-started/quick-start',
      ],
    },
    {
      type: 'category',
      label: 'Commands',
      items: [
        'commands/setup',
        'commands/agents',
        'commands/tools',
      ],
    },
    {
      type: 'category',
      label: 'Core Concepts',
      items: [
        'concepts/agentic-flows',
        'concepts/nodes',
        'concepts/tools',
        'concepts/yaml-configuration',
      ],
    },
    {
      type: 'category',
      label: 'Tutorials',
      items: [
        'tutorials/creating-agents',
        'tutorials/using-tools',
        'tutorials/advanced-flows',
      ],
    },
    {
      type: 'category',
      label: 'API Reference',
      items: [
        {
          type: 'category',
          label: 'Core',
          items: [
            'api/core/agent-runner',
          ],
        },
        {
          type: 'category',
          label: 'Tools',
          items: [
            'api/tools/internal-tools',
            'api/tools/mcp-tools',
          ],
        },
      ],
    },
  ],
};

export default sidebars;
