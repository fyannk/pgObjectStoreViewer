import {themes as prismThemes} from 'prism-react-renderer';
import type {Config} from '@docusaurus/types';
import type * as Preset from '@docusaurus/preset-classic';

// This runs in Node.js - Don't use client-side code here (browser APIs, JSX...)

const config: Config = {
  title: 'ObjectStoreViewer',
  tagline: 'Structural evidence about PostgreSQL backup repositories — never a restore guarantee',
  favicon: 'img/favicon.ico',

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

  // Docusaurus emits the favicon link but nothing for iOS home screens, so the
  // touch icon has to be declared here or the 180x180 asset is never requested.
  headTags: [
    {
      tagName: 'link',
      attributes: {
        rel: 'apple-touch-icon',
        sizes: '180x180',
        href: '/pgObjectStoreViewer/img/apple-touch-icon.png',
      },
    },
  ],

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
    // The card unfurled by chat clients and social previews: the mark and the
    // pgOSV lockup on the brand navy, so a link to the docs is recognisable
    // before the page loads.
    image: 'img/social-card.png',
    metadata: [
      {
        name: 'description',
        content:
          'ObjectStoreViewer turns PostgreSQL backup repositories in S3, Azure Blob Storage, and GCS into a bounded, read-only inventory: backups, WAL continuity, timelines, and observed recovery coverage.',
      },
    ],
    colorMode: {
      respectPrefersColorScheme: true,
    },
    navbar: {
      title: 'ObjectStoreViewer',
      logo: {
        alt: 'ObjectStoreViewer',
        src: 'img/logo.png',
        // The navbar is navy in both themes, so one file serves both. Without
        // srcDark, Docusaurus renders a light-theme-only image and the mark
        // vanishes in dark mode.
        srcDark: 'img/logo.png',
      },
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
        {
          href: 'https://github.com/fyannk/pgObjectStoreViewer',
          position: 'right',
          className: 'navbar-github',
          'aria-label': 'ObjectStoreViewer on GitHub',
        },
      ],
    },
    footer: {
      style: 'dark',
      logo: {
        alt: 'ObjectStoreViewer',
        src: 'img/logo.png',
        srcDark: 'img/logo.png',
        href: 'https://github.com/fyannk/pgObjectStoreViewer',
        width: 84,
      },
      copyright: `Copyright © ${new Date().getFullYear()} ObjectStoreViewer contributors. Apache-2.0 licensed.`,
      links: [
        {
          title: 'Project',
          items: [
            {
              label: 'GitHub',
              href: 'https://github.com/fyannk/pgObjectStoreViewer',
            },
            {
              label: 'Releases',
              href: 'https://github.com/fyannk/pgObjectStoreViewer/releases',
            },
            {
              label: 'Report a vulnerability',
              href: 'https://github.com/fyannk/pgObjectStoreViewer/security/policy',
            },
          ],
        },
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
