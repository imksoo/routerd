import type {SidebarsConfig} from '@docusaurus/plugin-content-docs';

const sidebars: SidebarsConfig = {
  tutorialSidebar: [
    {
      type: 'category',
      label: 'Tutorials',
      items: [
        'tutorials/getting-started',
        'tutorials/router-lab',
      ],
    },
    {
      type: 'category',
      label: 'Reference',
      items: [
        'api-v1alpha1',
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
    {
      type: 'category',
      label: 'Deployment',
      items: [
        'deployment/cloudflare-pages',
      ],
    },
  ],
};

export default sidebars;
