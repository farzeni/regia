import { defineConfig } from 'vitepress'

export default defineConfig({
  title: 'regia',
  description: 'A terminal control room for Claude Code background agents.',
  cleanUrls: true,
  lastUpdated: true,
  // conventions.md is for people and agents changing regia's code: it stays in the repo.
  srcExclude: ['conventions.md'],
  themeConfig: {
    nav: [
      { text: 'Guide', link: '/guide/getting-started' },
      { text: 'Reference', link: '/reference/keys' },
      { text: 'Design', link: '/design/agents-mcp' }
    ],
    sidebar: [
      {
        text: 'Guide',
        items: [
          { text: 'Getting started', link: '/guide/getting-started' },
          { text: 'The agents view', link: '/guide/agents' },
          { text: 'Inbox', link: '/guide/inbox' },
          { text: 'Search', link: '/guide/search' },
          { text: 'Starting agents', link: '/guide/starting-agents' },
          { text: 'Organizing the tree', link: '/guide/organizing' },
          { text: 'Group awareness', link: '/guide/group-awareness' },
          { text: 'Agents that organize regia', link: '/guide/agents-organize' },
          { text: 'Openers', link: '/guide/openers' },
          { text: 'Plugins', link: '/guide/plugins' }
        ]
      },
      {
        text: 'Reference',
        items: [
          { text: 'Keys', link: '/reference/keys' },
          { text: 'Commands', link: '/reference/commands' },
          { text: 'Plugin protocol', link: '/plugins' },
          { text: 'Data and files', link: '/reference/data' }
        ]
      },
      {
        text: 'Design',
        items: [{ text: 'Agents that organize regia', link: '/design/agents-mcp' }]
      }
    ],
    socialLinks: [{ icon: 'github', link: 'https://github.com/farzeni/regia' }],
    search: { provider: 'local' },
    outline: 'deep'
  }
})
