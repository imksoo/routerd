# routerd.net

HTML、Markdown、YAML、JSON Schemaをそのまま公開する静的サイトです。
コンフィグ作成ツール、React、Docusaurusは使用しません。
文書はGitHubのMarkdown表示で読み、設定例は `examples/` を参照します。

公開用ファイルは `sh build.sh` で `build/` にコピーします。
外部依存の取得、Markdown変換、言語別コンパイルはありません。

既存のCloudflare Pages設定（ルート `website`、コマンド
`npm ci && npm run build`、出力 `build`）もそのまま利用できます。
package.jsonはこの設定用の互換入口であり、npm依存はありません。
新規設定ではビルドコマンドを `sh build.sh` にできます。

文書は英語を `docs/`、他の3言語を `docs/ja/`、`docs/zh-Hant/`、
`docs/zh-Hans/` に置きます。リソース参照の更新は開発時に
`make generate-resource-docs` で行い、公開時には生成しません。

トップページは英語を `/`、日本語を `/ja/`、繁體中文を `/zh-Hant/`、
简体中文を `/zh-Hans/` で公開します。4ページは同じ構成の静的HTMLで、
共通の `static/site.css` を使います。各ページは対応する言語のMarkdownへ案内します。
