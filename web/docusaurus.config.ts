import {themes as prismThemes} from 'prism-react-renderer';
import type {Config} from '@docusaurus/types';
import type * as Preset from '@docusaurus/preset-classic';

// This runs in Node.js - Don't use client-side code here (browser APIs, JSX...)

const config: Config = {
  title: 'ObjectStoreViewer',
  tagline: 'Structural evidence about PostgreSQL backup repositories — never a restore guarantee',
  favicon: 'img/favicon.svg',

  // GitHub Pages for fyannk/pgObjectStoreViewer.
  url: 'https://fyannk.github.io',
  baseUrl: '/pgObjectStoreViewer/',
  trailingSlash: true,

  organizationName: 'fyannk',
  projectName: 'pgObjectStoreViewer',

  onBrokenLinks: 'throw',

  markdown: {
    mermaid: true,
    hooks: {
      onBrokenMarkdownLinks: 'throw',
    },
  },

  i18n: {
    defaultLocale: 'en',
    locales: ['en'],
  },

  presets: [
    [
      'classic',
      {
        docs: {
          path: 'docs',
          sidebarPath: './sidebars.ts',
          includeCurrentVersion: true,
          lastVersion: '0.1.1',
          versions: {
            current: {
              label: 'Dev',
              badge: true,
              banner: 'unreleased',
            },
            '0.1.1': {
              label: 'v0.1.1',
              badge: true,
              banner: 'none',
            },
            '0.1.0': {
              label: 'v0.1.0',
              badge: true,
              banner: 'none',
            },
          },
        },
        blog: false,
        theme: {
          customCss: './src/css/custom.css',
        },
      } satisfies Preset.Options,
    ],
  ],
  themes: [
    '@docusaurus/theme-mermaid',
    [
      require.resolve('@easyops-cn/docusaurus-search-local'),
      {
        hashed: true,
        docsDir: ['docs'],
        searchResultLimits: 8,
        searchResultContextMaxLength: 50,
        language: ['en'],
        indexBlog: false,
        indexPages: false,
      },
    ],
  ],
  themeConfig: {
    navbar: {
      title: 'ObjectStoreViewer',
      items: [
        {
          type: 'docSidebar',
          sidebarId: 'docs',
          position: 'left',
          label: 'Documentation',
        },
        {
          type: 'docsVersionDropdown',
          position: 'right',
        },
      ],
    },
    footer: {
      style: 'dark',
      links: [
        {
          title: 'Backup tooling',
          items: [
            {
              label: 'Barman',
              href: 'https://docs.pgbarman.org/',
            },
            {
              label: 'pgBackRest',
              href: 'https://pgbackrest.org/',
            },
          ],
        },
        {
          title: 'Kubernetes',
          items: [
            {
              label: 'CloudNativePG',
              href: 'https://cloudnative-pg.io',
            },
            {
              label: 'pgToolBox',
              href: 'https://fyannk.github.io/pgtoolbox/',
            },
          ],
        },
      ],
    },
    prism: {
      theme: prismThemes.github,
      darkTheme: prismThemes.dracula,
      additionalLanguages: ['bash', 'yaml', 'go', 'json'],
    },
  } satisfies Preset.ThemeConfig,
};

export default config;
