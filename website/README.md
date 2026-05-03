# routerd.net

This directory contains the Docusaurus site for `routerd.net`.

The English docs source is the repository `docs/` directory. Japanese translated
docs live under `website/i18n/ja/docusaurus-plugin-content-docs/current/`.

Cloudflare Pages settings:

- Root directory: `website`
- Build command: `npm ci && npm run build`
- Build output directory: `build`
- Node.js version: `22`

Local build:

```bash
npm ci
npm run build
```

Run this after documentation rewrites because Docusaurus catches broken links and
frontmatter errors that Markdown-only checks miss.
