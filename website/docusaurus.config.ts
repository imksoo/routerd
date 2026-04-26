import type {Config} from '@docusaurus/types';
import type {Options as PresetOptions, ThemeConfig} from '@docusaurus/preset-classic';

const config: Config = {
  title: 'routerd',
  tagline: 'Declarative router control for small, serious networks',
  favicon: 'img/favicon.svg',

  url: 'https://routerd.net',
  baseUrl: '/',

  organizationName: 'imksoo',
  projectName: 'routerd',
  trailingSlash: false,

  onBrokenLinks: 'throw',
  onBrokenMarkdownLinks: 'warn',

  i18n: {
    defaultLocale: 'en',
    locales: ['en', 'ja'],
    localeConfigs: {
      en: {
        label: 'English',
      },
      ja: {
        label: '日本語',
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
          exclude: ['**/*.ja.md'],
          editUrl: 'https://github.com/imksoo/routerd/edit/main/docs/',
        },
        blog: false,
        theme: {
          customCss: './src/css/custom.css',
        },
      } satisfies PresetOptions,
    ],
  ],

  themeConfig: {
    image: 'img/routerd-social-card.svg',
    navbar: {
      title: 'routerd',
      logo: {
        alt: 'routerd',
        src: 'img/logo.svg',
      },
      items: [
        {to: '/docs/tutorials/getting-started', label: 'Tutorial', position: 'left'},
        {to: '/docs/reference/api-v1alpha1', label: 'Resources', position: 'left'},
        {to: '/docs/releases/changelog', label: 'Changelog', position: 'left'},
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
            {label: 'Getting Started', to: '/docs/tutorials/getting-started'},
            {label: 'Resource API', to: '/docs/reference/api-v1alpha1'},
            {label: 'Plugin Protocol', to: '/docs/reference/plugin-protocol'},
          ],
        },
        {
          title: 'Project',
          items: [
            {label: 'GitHub', href: 'https://github.com/imksoo/routerd'},
            {label: 'Changelog', to: '/docs/releases/changelog'},
          ],
        },
      ],
      copyright: `Copyright © ${new Date().getFullYear()} routerd contributors.`,
    },
    prism: {
      additionalLanguages: ['bash', 'go', 'yaml', 'json'],
    },
  } satisfies ThemeConfig,
};

export default config;
