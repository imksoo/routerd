// SPDX-License-Identifier: BSD-3-Clause

import type {Config} from '@docusaurus/types';
import type {Options as PresetOptions, ThemeConfig} from '@docusaurus/preset-classic';

const config: Config = {
  title: 'routerd',
  tagline: 'A declarative router for small but serious networks',
  favicon: 'img/favicon.svg',

  url: 'https://routerd.net',
  baseUrl: '/',

  organizationName: 'imksoo',
  projectName: 'routerd',
  trailingSlash: false,
  markdown: {
    mermaid: true,
    hooks: {
      onBrokenMarkdownLinks: 'warn',
    },
  },
  themes: ['@docusaurus/theme-mermaid'],
  plugins: [
    function suppressCodeBlockLanguageServerWarning() {
      return {
        name: 'suppress-codeblock-language-server-warning',
        configureWebpack() {
          return {
            ignoreWarnings: [
              {
                module: /vscode-languageserver-types/,
                message: /Critical dependency: require function is used/,
              },
            ],
          };
        },
      };
    },
  ],

  onBrokenLinks: 'warn',

  i18n: {
    defaultLocale: 'en',
    // Locale order is cosmetic (it drives the locale dropdown order).
    locales: ['en', 'ja', 'zh-Hans', 'zh-Hant'],
    localeConfigs: {
      en: {
        label: 'English',
      },
      ja: {
        label: '日本語',
      },
      'zh-Hant': {
        label: '繁體中文',
      },
      'zh-Hans': {
        label: '简体中文',
      },
    },
  },

  presets: [
    [
      'classic',
      {
        docs: {
          path: '../docs',
          routeBasePath: 'docs',
          sidebarPath: './sidebars.ts',
          exclude: ['**/*.ja.md', 'internal/**'],
          editUrl: 'https://github.com/imksoo/routerd/edit/main/docs/',
        },
        blog: {
          showReadingTime: true,
          routeBasePath: 'blog',
          blogTitle: 'routerd field notes',
          blogDescription: 'Practical routerd walkthroughs and design notes.',
        },
        theme: {
          customCss: './src/css/custom.css',
        },
      } satisfies PresetOptions,
    ],
  ],

  themeConfig: {
    image: 'img/routerd-social-card.svg',
    announcementBar: {
      id: 'stable-milestone-20260707-1514',
      content:
        'Stable milestone: <a href="/docs/releases/stable"><b>v20260707.1514</b></a> — AWS/Azure/OCI/PVE redundant full topology 56/56',
      backgroundColor: '#1f6feb',
      textColor: '#ffffff',
      isCloseable: true,
    },
    navbar: {
      title: 'routerd',
      logo: {
        alt: 'routerd',
        src: 'img/logo.svg',
      },
      items: [
        {
          type: 'dropdown',
          label: 'Docs',
          position: 'left',
          items: [
            {to: '/docs/', label: 'Overview'},
            {to: '/docs/install-and-upgrade', label: 'Install'},
            {to: '/wizard', label: 'Config wizard'},
            {to: '/docs/concepts/resource-model', label: 'Resource model'},
            {to: '/docs/concepts/glossary', label: 'Glossary'},
            {to: '/docs/concepts/firewall', label: 'Build a router'},
            {to: '/docs/config-examples/', label: 'Configuration examples'},
            {to: '/docs/how-to/multi-wan', label: 'How-to guides'},
            {to: '/docs/operations/routerctl-doctor', label: 'Operate'},
            {to: '/docs/reference/api-v1alpha1', label: 'API reference'},
            {to: '/docs/design-notes', label: 'Design notes'},
            {to: '/docs/releases/stable', label: 'Releases'},
          ],
        },
        {
          type: 'localeDropdown',
          position: 'right',
        },
        {
          href: 'https://github.com/imksoo/routerd',
          label: 'GitHub',
          position: 'right',
        },
      ],
    },
    footer: {
      style: 'dark',
      links: [
        {
          title: 'Docs',
          items: [
            {label: 'Install & Upgrade', to: '/docs/install-and-upgrade'},
            {label: 'Getting Started', to: '/docs/tutorials/getting-started'},
            {label: 'Resource API', to: '/docs/reference/api-v1alpha1'},
            {label: 'Plugin Protocol', to: '/docs/reference/plugin-protocol'},
          ],
        },
        {
          title: 'Project',
          items: [
            {label: 'GitHub', href: 'https://github.com/imksoo/routerd'},
            {label: 'Stable milestone', to: '/docs/releases/stable'},
            {label: 'Changelog', to: '/docs/releases/changelog'},
          ],
        },
      ],
      copyright: `Copyright © ${new Date().getFullYear()} Kirino Minato and routerd contributors. Licensed under the BSD 3-Clause License.`,
    },
    prism: {
      additionalLanguages: ['bash', 'go', 'yaml', 'json'],
    },
  } satisfies ThemeConfig,
};

export default config;
