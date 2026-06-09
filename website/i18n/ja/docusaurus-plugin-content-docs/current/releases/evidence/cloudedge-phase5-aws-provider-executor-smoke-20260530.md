# CloudEdge Phase 5.1 AWS プロバイダーエグゼキュータースモーク

Result: PASS

日付: 2026-05-31 UTC  
ブランチ/ビルド: `main` / `routerd v20260528.2308 (92f4cc94)` (`execute.providerAction` のローカルバリデータ修正付き)  
エビデンスバンドル: `/home/imksoo/routerd-labs/cloudedge-sam/evidence/20260530T235341Z-phase5-aws-rebaseline-92f4cc94`

## スコープ

- プロバイダー変更操作の対象: AWS のみ。
- アカウント/リージョン: `350538780953` / `ap-northeast-1`。
- 再利用した routerd 専用 SAM ラボ: `SourceLab=routerd-cloudedge-sam-aws-pve`。
- 対象ルーターインスタンス: `routerd-cloud-aws` / `i-05b6cfd2b3e4e0da6`。
- 対象クライアントインスタンス: `aws-cloud-client` / `i-0ae791389518353d6`。
- 対象 ENI: `eni-0904ccbed8d383f65`。
- 捕捉アドレス: `10.88.60.9`。

## 基準値リセット

変更操作の前に、既存の SAM ラボを初期状態のプロバイダー基準値にリセット:

- `eni-0904ccbed8d383f65` から `10.88.60.9` セカンダリプライベート IP を削除。
- ENI で `SourceDestCheck=true` を復元。
- リセット後エビデンス: `aws-router-eni-post-reset.json`、`aws-router-eni-post-reset-confirm.json`。

## IAM ゲート

`routerd-cloud-aws` がエグゼキューター用の EC2 インスタンスプロファイルを受領。

インラインポリシーで許可されたのは以下のみ:

- `ec2:DescribeNetworkInterfaces`
- `ec2:AssignPrivateIpAddresses`
- `ec2:UnassignPrivateIpAddresses`
- `ec2:ModifyNetworkInterfaceAttribute`

変更操作権限のスコープ:

- リージョン: `ap-northeast-1`
- ENI ARN: `arn:aws:ec2:ap-northeast-1:350538780953:network-interface/eni-0904ccbed8d383f65`
- リソースタグ: `Project=routerd-cloudedge-phase5`

ルーターからのインスタンスロール preflight が通過:

- `aws sts get-caller-identity` が `arn:aws:sts::350538780953:assumed-role/routerd-phase5-aws-executor-role/i-05b6cfd2b3e4e0da6` を返却。
- `aws ec2 describe-network-interfaces` で対象 ENI を読み取り可能。

## エグゼキューター実行

`aws-provider-executor` を `routerd-cloud-aws` にビルドしインストール。

2 つのアクションジャーナルエントリをインポート、承認、dry-run、実行:

- `assign-secondary-ip`
  - Result: `succeeded`
  - Message: `assigned 10.88.60.9 to eni-0904ccbed8d383f65`
- `ensure-forwarding-enabled`
  - Result: `succeeded`
  - Message: `disabled SourceDestCheck on eni-0904ccbed8d383f65 (prior=true)`
  - 観測されたジャーナルファクト: `priorSourceDestCheck=true`

変更操作後の AWS 検証:

- ENI プライマリ: `10.88.60.4`
- ENI セカンダリ: `10.88.60.9`
- `SourceDestCheck=false`

## データプレーン検証

クラウド側:

- `routerctl doctor hybrid`: `overall=pass`、`pass=12`、`warn=0`、`fail=0`、`skip=1`。
- 配送ルート: `10.88.60.9 dev wg-hybrid metric 120`。
- ローカル OS アドレス不在: `10.88.60.9/32 absent from local interfaces`。
- MSS clamp: `routerd_mss covers ens5 -> wg-hybrid`。

オンプレミス側:

- router07 `routerctl doctor hybrid`: `overall=pass`、`pass=13`、`warn=0`、`fail=0`、`skip=1`。
- クラウドクライアント `10.88.60.7` の Proxy ARP claim は健全なまま。

クライアント接続性:

- cloud-client `10.88.60.7` -> onprem-client `10.88.60.9` ping: `3/3`、`0% packet loss`。
- onprem-client `10.88.60.9` -> cloud-client `10.88.60.7` ping: `3/3`、`0% packet loss`。
- cloud -> onprem SSH ソース保持:
  - `SSH_CONNECTION=10.88.60.7 ... 10.88.60.9 22`
- onprem -> cloud SSH ソース保持:
  - `SSH_CONNECTION=10.88.60.9 ... 10.88.60.7 22`
- デフォルトゲートウェイ変更なし:
  - cloud-client: `default via 10.88.60.1 dev ens5`
  - onprem-client: `default via 10.88.60.1 dev eth0`
- NAT: SSH ソース保持により不在を確認。

## ロールバックとリストア

`routerctl action rollback` によるロールバックを実施:

- `ensure-forwarding-enabled` ロールバック dry-run: `SourceDestCheck` を再有効化予定。
- `assign-secondary-ip` ロールバック dry-run: `10.88.60.9` を割当解除予定。
- ライブロールバック結果:
  - action 2: `rolledBack`、`SourceDestCheck=true` を復元。
  - action 1: `rolledBack`、`10.88.60.9` を割当解除。

最終後片付けはオプション B を使用: 既存の SAM ラボ状態を復元。

- `10.88.60.9` セカンダリプライベート IP が再び存在。
- `SourceDestCheck=false`。
- `routerd-cloud-aws`: `stopped`。
- `aws-cloud-client`: `stopped`。

コスト状態:

- EC2 コンピューティング停止済み。
- 既存の EIP/ディスク/NIC/VPC ラボリソースは再利用可能な SAM ラボ状態として残存。

## ノート

- 実行中にコードバグを発見しローカルで修正: `PluginSpec` スキーマとエグゼキューターリゾルバは `execute.providerAction` をサポートしていたが、`pkg/config/validate_plugin.go` がまだ拒否していた。
- forwarding アクションにも `target.address=10.88.60.9` が必要だった。これにより `ProviderActionPolicy.allowedCIDRs` がポリシーを弱めずにアクションをゲートできるようになった。
