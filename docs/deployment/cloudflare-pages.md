---
title: Cloudflare Pages Deployment
---

# Cloudflare Pages Deployment

routerd.net is intended to be published with Cloudflare Pages.

Recommended Cloudflare Pages settings:

| Setting | Value |
| --- | --- |
| Production branch | `main` |
| Root directory | `website` |
| Build command | `npm ci && npm run build` |
| Build output directory | `build` |
| Node.js version | `22` |

The Docusaurus site builds English pages at `/` and Japanese pages at `/ja/`.

## Custom Domain

Add `routerd.net` as a custom domain in the Cloudflare Pages project. Because
the domain is managed by Cloudflare DNS, Cloudflare can create or validate the
required DNS records from the Pages dashboard.

No `CNAME` file is required. That file is a GitHub Pages convention and should
not be used for Cloudflare Pages deployments.

## Local Build

```bash
cd website
npm ci
npm run build
```

The static output is written to `website/build`.

## Preview Deployments

Cloudflare Pages creates preview deployments for non-production branches and
pull requests when the project is connected to the GitHub repository. This is
useful for reviewing documentation changes before merging them to `main`.
