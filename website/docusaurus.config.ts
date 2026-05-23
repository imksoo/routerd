// SPDX-License-Identifier: BSD-3-Clause

import type {Config} from '@docusaurus/types';
import type {Options as PresetOptions, ThemeConfig} from '@docusaurus/preset-classic';

const config: Config = {
  title: 'routerd',
  tagline: '小さく本気のネットワークのための、宣言型ルーター',
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
    defaultLocale: 'ja',
    locales: ['ja', 'en', 'zh-Hant', 'zh-Hans'],
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
      id: 'stable-milestone-20260522-1334',
      content:
        '安定版マイルストーン / Stable: <a href="/docs/releases/stable"><b>v20260522.1334</b></a>（本番稼働実績あり / running in production）',
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
          label: 'ドキュメント',
          position: 'left',
          items: [
            {to: '/docs/', label: 'はじめに'},
            {to: '/docs/install-and-upgrade', label: '導入（クイックスタート）'},
            {to: '/docs/concepts/resource-model', label: '機能解説（宣言型モデル）'},
            {to: '/docs/concepts/glossary', label: '用語集'},
            {to: '/docs/concepts/firewall', label: '設定リファレンス（機能別）'},
            {to: '/docs/config-examples/', label: '設定例集'},
            {to: '/docs/how-to/multi-wan', label: 'How-to ガイド'},
            {to: '/docs/operations/reconcile', label: '運用'},
            {to: '/docs/reference/api-v1alpha1', label: 'リファレンス（API）'},
            {to: '/docs/design-notes', label: '設計ノート'},
            {to: '/docs/releases/stable', label: 'リリースと安定版'},
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
          title: 'ドキュメント',
          items: [
            {label: '導入とアップグレード', to: '/docs/install-and-upgrade'},
            {label: 'クイックスタート', to: '/docs/tutorials/getting-started'},
            {label: 'リソース API', to: '/docs/reference/api-v1alpha1'},
            {label: 'プラグインプロトコル', to: '/docs/reference/plugin-protocol'},
          ],
        },
        {
          title: 'プロジェクト',
          items: [
            {label: 'GitHub', href: 'https://github.com/imksoo/routerd'},
            {label: '安定版マイルストーン', to: '/docs/releases/stable'},
            {label: '変更履歴', to: '/docs/releases/changelog'},
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
