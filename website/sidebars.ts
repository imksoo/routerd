import type {SidebarsConfig} from '@docusaurus/plugin-content-docs';

const sidebars: SidebarsConfig = {
  tutorialSidebar: [
    {
      type: 'category',
      label: 'Concepts',
      collapsed: false,
      items: [
        'concepts/what-is-routerd',
        'concepts/design-philosophy',
        'concepts/resource-model',
        'concepts/apply-and-render',
        'concepts/state-and-ownership',
      ],
    },
    {
      type: 'category',
      label: 'Tutorials',
      collapsed: false,
      items: [
        'tutorials/getting-started',
        'tutorials/install',
        'tutorials/first-router',
        'tutorials/lan-side-services',
        'tutorials/basic-firewall',
        'tutorials/router-lab',
        'tutorials/nixos-getting-started',
      ],
    },
    {
      type: 'category',
      label: 'How-to',
      items: [
        'how-to/flets-ipv6-setup',
        'how-to/troubleshooting',
      ],
    },
    {
      type: 'category',
      label: 'Knowledge base',
      items: [
        'knowledge-base/dhcpv6-pd-clients',
      ],
    },
    {
      type: 'category',
      label: 'Reference',
      items: [
        'api-v1alpha1',
        'resource-ownership',
        'control-api-v1alpha1',
        'plugin-protocol',
        'platforms',
      ],
    },
    {
      type: 'category',
      label: 'Operations',
      items: [
        'operations/state-database',
        'operations/inventory',
      ],
    },
    {
      type: 'category',
      label: 'Design notes',
      collapsed: true,
      items: [
        'design-notes',
      ],
    },
    {
      type: 'category',
      label: 'Releases',
      items: [
        'releases/changelog',
      ],
    },
  ],
};

export default sidebars;
