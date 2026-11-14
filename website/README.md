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
