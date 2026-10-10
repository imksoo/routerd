---
title: 開発時の確認
---

# 開発時の確認

![Diagram showing development checks split between local pre-commit tests, CI pull request validation, and release workflow archive publishing](/img/diagrams/operations-development.png)

routerd は 2 つの自動化経路を分けています。

- CI ワークフローは通常の push と pull request を検証します。
- Release ワークフローはリリースタグを push した後にリリースアーカイブを生成します。

Release ワークフローは複数の OS と CPU アーキテクチャーを対象にします。
また、GitHub Release の成果物も公開します。
そのため、通常の CI とは分けています。


## 試験・ハーネス設計の必須規範

単体試験、オフライン fixture、runtime smoke、release qualification、再利用可能な旧 helper・テンプレート・コピー元にも適用します。現在の caller がないコードも対象です。変更時は製品要件、必要な直接証拠、観測の限界を明示しなければなりません。

- 観測・診断ログは残し、製品合否から分離します。具体的な製品要件なしに journal、進展カウンタ、latency 統計、ホスト全体の診断を合否条件へ結合してはいけません。
- PASS／製品 FAIL は完了済みで対象へ帰属できる直接証拠で判断します。API／SSH の揺らぎ、採取不能、欠落・壊れた入力、guest command 未完了、timeout は観測／インフラの結果です。ゼロ、違反なし、拒否成功、実測製品違反へ変換してはいけません。
- 無根拠な速度条件、重複 probe・gate を削減します。契約上の期限と停止用の有界 watchdog は維持し、製品性能の実測と区別します。
- 一時データを削除する前に、失敗した生の試行、ソース識別、exit、時刻、cleanup 結果を保持します。今回所有する資源だけを cleanup し、失敗を隠したり製品証拠を消したりしてはいけません。
- 原因調査せず失敗した実試験を反復してはいけません。既存 retry・時間・費用予算に達したら停止し、失敗・不確実性・残予算・修正案を報告してから次へ進みます。根拠のある失敗／欠落観測だけを有界に再取得し、各試行を保存します。
- 実際の入口、選択ソースのパスと SHA、依存関係、最終結果への伝播を監査します。候補や mock の検証はその範囲の証拠です。稼働入口が候補を選ぶことや実転送の証明とは扱えません。
- 実装、準備候補、稼働コード、保存原本の再解析、意図的に変更した fixture、未実証を区別します。原本の負例を保持し、過去の失敗を PASS に書き換えてはいけません。
- 未接続・旧コードの再利用にも同じ規範を適用します。封印原本は不変とし、再利用されるソースを直します。現役 caller がないことを理由に欠陥を無視してはいけません。

真の機能要件、安全・所有権の確認、費用制限を弱める規範ではありません。必要証拠の欠落は PASS の根拠になりません。正常通信と直接違反の証明は維持し、診断との無根拠な結合だけを外します。

良い例: curl exit 0／HTTP 200 と必要な body・経路証拠があれば、任意の latency 欠落は診断として記録し PASS にできます。悪い例: WireGuard 取得失敗を「禁止 AllowedIP なし」と扱う、invalid apply の timeout／強制終了を拒否成功に数える、同一スタックの ping 成功をトンネル転送の証明にする。

## CI ワークフロー

`.github/workflows/ci.yaml` はブランチへの push と pull request で動きます。
Ubuntu のランナーを使い、レビュー前に緑に保つべき範囲を確認します。

```sh
go test ./...
make check-schema
make validate-example
make website-build
```

`webconsole/`、チェックイン済みの静的 asset、共通 quality workflow、または Makefile を
変更した場合、CI は Web Console 専用 gate も実行します。`npm ci`、全依存と本番依存の
high severity audit、TypeScript typecheck、本番 build、生成 asset の差分検査を行います。

CI ワークフローはリリース成果物を公開しません。
リリースアーカイブは、日付ベースのタグで `Release` ワークフローが生成します。

## pre-commit フック

リポジトリには任意で使える pre-commit フックを含めています。

```sh
ln -sf ../../scripts/pre-commit.sh .git/hooks/pre-commit
chmod +x scripts/pre-commit.sh
```

有効にすると、`git commit` の前に次の確認を実行します。

```sh
go test ./...
make check-schema
```

どちらかが失敗するとコミットは止まります。
スキーマの差分やテスト失敗を CI の前に検出できます。

緊急でローカルコミットが必要な場合は、次の環境変数を指定します。

```sh
ROUTERD_SKIP_PRE_COMMIT=1 git commit
```

後続の修正が明確な場合だけ使ってください。
ブランチを push した後は CI が実行されます。
