---
title: Cloudflare Pages での公開
---

# Cloudflare Pages での公開

routerd.net は Cloudflare Pages で公開する前提です。

推奨する Cloudflare Pages 設定:

| Setting | Value |
| --- | --- |
| Production branch | `main` |
| Root directory | `website` |
| Build command | `npm ci && npm run build` |
| Build output directory | `build` |
| Node.js version | `22` |

Docusaurus site は英語版を `/`、日本語版を `/ja/` に build します。

## Custom Domain

Cloudflare Pages project に `routerd.net` を custom domain として追加します。domain も Cloudflare DNS で管理しているため、Pages dashboard から必要な DNS record の作成または検証を進められます。

`CNAME` file は不要です。これは GitHub Pages の convention であり、Cloudflare Pages deployment では使いません。

## Local Build

```bash
cd website
npm ci
npm run build
```

静的出力は `website/build` に生成されます。

## Preview Deployments

Cloudflare Pages project を GitHub repository と連携すると、production branch 以外や pull request から preview deployment が作られます。documentation change を `main` に merge する前に確認できます。
