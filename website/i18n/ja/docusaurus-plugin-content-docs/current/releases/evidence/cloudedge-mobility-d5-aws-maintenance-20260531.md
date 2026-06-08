# CloudEdge Mobility D5 AWS メンテナンススモーク

Result: PASS

日付: 2026-05-31
ビルド: main 99eb1d45
エビデンスバンドル: `/home/imksoo/routerd-labs/cloudedge-mobility/evidence/20260531T215831Z-d5-aws-rerun-99eb1d45`

## シナリオ

- AWS のみの D5 ライブメンテナンス / キャプチャマイグレーション。
- 既存のアクティブルーター A を再利用: `i-001f62ac01d66e782`、ENI-A `eni-0d17f203a6717e4d9`、プライマリ `10.77.60.4`。
- スタンバイルーター B をこの実行用に再作成: `i-045382a4f5bbf6fc0`、ENI-B `eni-017dd140722f5d819`、プライマリ `10.77.60.14`、`t3.small`。
- AWS クラウドクライアントを再利用: `i-0c5d4e3578e7669a9`、`10.77.60.11`。
- キャプチャアドレス: オンプレミスクライアント `10.77.60.10/32`。

## 初期キャプチャ

- A がインポートして実行:
  - `assign-secondary-ip` epoch 1 (`10.77.60.10/32` を ENI-A に)。
  - `ensure-forwarding-enabled` epoch 1 (ENI-A に対して)。
- 初期 execute 後の AWS プロバイダー状態:
  - ENI-A: `10.77.60.4,10.77.60.10`、`SourceDestCheck=false`。
  - ENI-B: `10.77.60.14`、`SourceDestCheck=true`。
- マイグレーション前のデータプレーン:
  - cloud-client `10.77.60.11 -> 10.77.60.10` ping: `3/3`、`0% loss`。
  - SSH でオンプレミスクライアントにソース保持で到達: `SSH_CONNECTION=10.77.60.11 ... 10.77.60.10 22`。

## ドレインとマイグレーション

- ルーター A に `maintenance.drain=true` を宣言的に適用。
- A が epoch 2 の de-provision アクションをインポート:
  - `unassign-secondary-ip` (`10.77.60.10/32` を ENI-A から)。
  - `ensure-forwarding-disabled` (ENI-A に対して)。
- B が epoch 2 の active-capture アクションをインポート:
  - `assign-secondary-ip` (`10.77.60.10/32` を ENI-B に)。
  - `ensure-forwarding-enabled` (ENI-B に対して)。
- A の unassign が正常に実行され、ENI-A から `.10` を削除。
- B の assign が正常に実行され、ENI-B に `.10` を追加。
- マイグレーション後の AWS プロバイダー状態:
  - ENI-A: `10.77.60.4`、`SourceDestCheck=true`。
  - ENI-B: `10.77.60.14,10.77.60.10`、`SourceDestCheck=false`。
- キャプチャ epoch がホルダー `aws-router-b`、epoch `2` に収束。

## Epoch フェンス

- A の epoch 1 アクションはドレイン前に成功。
- A の epoch 2 unassign と forwarding-disable は実行されるまでジャーナルに残存。
- B の epoch 2 assign と forwarding-enable が正常に実行。
- stale ゲートを非プロバイダーのジャーナルプローブで検証:
  - 同一キャプチャキーの epoch 1 pending アクションを `d5-rerun-stale-probe-epoch1` として挿入;
  - `routerctl action import` が `status=skipped` に変更;
  - 結果メッセージ: `stale mobility capture epoch`。

## マイグレーション後のデータプレーン

- B 側 `doctor hybrid`: PASS。
- B 側 `routerd_mss`: `ens5 -> wg-hybrid` に存在。
- オンプレミス `routerd_mss`: `ens21 -> wg-hybrid` に存在。
- neighbor リフレッシュ後、cloud-client `10.77.60.11 -> 10.77.60.10` ping が 3 回連続ラウンドで `3/3` パス。
- SSH で B 経由でオンプレミスクライアントにソース保持で到達:
  - `SSH_CONNECTION=10.77.60.11 ... 10.77.60.10 22`。
- クライアントのデフォルトゲートウェイは変更なし: `default via 10.77.60.1`。

## Teardown

- ENI-B から `10.77.60.10` を削除。
- ENI-A と ENI-B で `SourceDestCheck=true` を復元。
- IAM インラインポリシーを B スコープ前のドキュメントに復元。
- B を terminate。
- A と cloud-client を停止。
- 最終コスト状態:
  - A: `stopped`。
  - cloud-client: `stopped`。
  - B: `terminated`。
  - ENI-A ベースライン: `10.77.60.4` のみ、`SourceDestCheck=true`。
