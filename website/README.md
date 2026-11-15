# routerd.net

HTML、Markdown、YAML、JSON Schemaを公開する静的サイトです。
コンフィグ作成ツール、React、Docusaurusは使用しません。
文書はサイト内の静的HTMLで読み、設定例は `examples/` から取得できます。

文書の正は `docs/` のMarkdownです。英語を `docs/`、他の3言語を
`docs/ja/`、`docs/zh-Hant/`、`docs/zh-Hans/` に置きます。
文書のHTMLはリポジトリに置かず、公開時に生成します。

公開用ファイルは `sh build.sh` で `build/` に作ります。静的ファイル、
Markdown、設定例をコピーし、`scripts/render-docs.py` で各Markdownの隣に
言語切替とページ内目次を含むHTMLを生成します。必要なものはPython 3と
`Markdown==3.11.3` だけで、未導入の場合は `build.sh` がpipで導入します。

既存のCloudflare Pages設定（ルート `website`、コマンド
`npm ci && npm run build`、出力 `build`）をそのまま利用します。
package.jsonはこの設定用の入口であり、npm依存はありません。
新規設定ではビルドコマンドを `sh build.sh` にできます。

リソース参照のMarkdownは開発時に `make generate-resource-docs` で更新して
コミットします。公開時には生成しません。

トップページは英語を `/`、日本語を `/ja/`、繁體中文を `/zh-Hant/`、
简体中文を `/zh-Hans/` で公開します。4ページは同じ構成の静的HTMLで、
共通の `static/site.css` を使います。各ページは対応する言語の文書HTMLへ案内します。
