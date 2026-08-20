# routerd.net

This directory contains the Docusaurus site for `routerd.net`.

The English docs source is the repository `docs/` directory. Localized docs
live under `website/i18n/<locale>/docusaurus-plugin-content-docs/current/`.

For beginner-facing changes, start by checking that the Japanese explanation is
natural and understandable to a network-curious student, then keep the English,
Traditional Chinese, and Simplified Chinese core onboarding pages equivalent in
meaning. The source layout stays English for Docusaurus mechanics; this is an
editorial review order, not a claim that a translation is less important.

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
