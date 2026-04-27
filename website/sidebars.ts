import type {SidebarsConfig} from '@docusaurus/plugin-content-docs';

const sidebars: SidebarsConfig = {
  tutorialSidebar: [
    {
      type: 'category',
      label: 'Tutorials',
      items: [
        'tutorials/getting-started',
        'tutorials/router-lab',
        'tutorials/nixos-getting-started',
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
