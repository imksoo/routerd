# CloudEdge Phase 5.1 OCI Provider Executor スモーク

Result: PASS

日付: 2026-05-31 UTC  
ブランチ/ビルド: `phase5-oci-azure-executors` / `routerd v20260528.2308 (67d96103)`  
エビデンスバンドル: `/home/imksoo/routerd-labs/cloudedge-sam/evidence/20260531T005414Z-phase5-oci-live-67d96103`

## スコープ

- プロバイダーミューテーション対象: OCI のみ。
- テナンシー/リージョン: `ocid1.tenancy.oc1..aaaaaaaaby2raoa2kzgywrsz6ofjk4eks6uwtpczgtqxulach3xgksfx52qq` / `ap-tokyo-1`。
- 再利用した routerd 専用 SAM ラボ: `Project=routerd-cloudedge-sam-oci-pve`。
- 対象ルーターインスタンス: `routerd-cloud-oci` / `ocid1.instance.oc1.ap-tokyo-1.anxhiljr6yebb3qc2sucs3kor7u77ki2cg7zf3xlgmubj5utwfqeejmm7crq`。
- 対象クライアントインスタンス: `oci-cloud-client` / `ocid1.instance.oc1.ap-tokyo-1.anxhiljr6yebb3qc2biuwl7yyjglwn6aompawzlfmkohpbrqceuijiuf7dva`。
- 対象 VNIC: `ocid1.vnic.oc1.ap-tokyo-1.abxhiljrzn6c2b4hs2jljbs4cmbshywzr7ldugepftjdrvm77nlvcvbdzzkq`。
- キャプチャアドレス: `10.77.60.9`。

## リベースライン

ミューテーション前に、既存の SAM ラボをフレッシュなプロバイダーベースラインにリセット:

- ルーター VNIC から `10.77.60.9` セカンダリプライベート IP を削除。
- VNIC で `skipSourceDestCheck=false` を復元。
- リセット後エビデンス: `oci-router-vnic-post-reset.json`、`oci-router-private-ips-post-reset.json`、`retry-reset-summary.tsv`。

## インスタンスプリンシパルゲート

`routerd-cloud-oci` が executor 用の OCI ダイナミックグループとポリシーを受領。

- ダイナミックグループ: `routerd_phase5_oci_executor`。
- 初期の最小権限ポリシーは `private-ip create` に不十分で、`NotAuthorizedOrNotFound` を返却。
- 進行優先の修正: このラボのダイナミックグループに対してポリシーを `manage virtual-network-family in tenancy` に拡大。

ルーターからのインスタンスプリンシパル preflight がパス:

- `oci network vnic get` で対象 VNIC を読み取り可能。
- `oci network private-ip list` で対象 VNIC のプライベート IP を読み取り可能。

## Executor 実行

`oci-provider-executor` を `routerd-cloud-oci` にビルドしインストール。

2 つの retry2 アクションジャーナルエントリをインポート、承認、dry-run、実行:

- `assign-secondary-ip`
  - Result: `succeeded`
  - Message: `assigned 10.77.60.9 to <target VNIC>`
- `ensure-forwarding-enabled`
  - Result: `succeeded`
  - Message: `set skipSourceDestCheck=true on <target VNIC> (prior=false)`
  - 観測されたジャーナルファクト: `priorSkipSourceDestCheck=false`

ミューテーション後の OCI 検証:

- VNIC プライマリ: `10.77.60.4`
- VNIC セカンダリ: `10.77.60.9`
- `skipSourceDestCheck=true`

## データプレーン検証

クラウド側:

- `routerctl doctor hybrid`: `overall=pass`、`pass=12`、`warn=0`、`fail=0`、`skip=1`。
- 配送ルート: `10.77.60.9 dev wg-hybrid metric 120`。
- ローカル OS アドレス不在: `10.77.60.9/32 absent from local interfaces`。
- MSS clamp: `routerd_mss covers ens3 -> wg-hybrid`。

オンプレミス側:

- router06 `routerctl doctor hybrid`: `overall=pass`、`pass=15`、`warn=0`、`fail=0`、`skip=1`。
- クラウドクライアント `10.77.60.7` の Proxy ARP claim は健全なまま。

クライアント接続性:

- cloud-client `10.77.60.7` -> onprem-client `10.77.60.9` ping: `3/3`、`0% packet loss`。
- onprem-client `10.77.60.9` -> cloud-client `10.77.60.7` ping: `3/3`、`0% packet loss`。
- cloud -> onprem SSH ソース保持:
  - `SSH_CONNECTION=10.77.60.7 ... 10.77.60.9 22`
- onprem -> cloud SSH ソース保持:
  - `SSH_CONNECTION=10.77.60.9 ... 10.77.60.7 22`
- デフォルトゲートウェイ変更なし:
  - cloud-client: `default via 10.77.60.1 dev ens3`
  - onprem-client: `default via 10.77.60.1 dev eth0`
- NAT: SSH ソース保持により不在を確認。

## ロールバックとリストア

`routerctl action rollback` によるロールバックを実施:

- action 4 `ensure-forwarding-enabled`: `rolledBack`、`skipSourceDestCheck=false` を復元。
- action 3 `assign-secondary-ip`: `rolledBack`、`10.77.60.9` を割当解除。

ロールバック中に修正可能なラボの問題が 1 件発見: OCI の `private-ip delete` が Plugin の元の `30s` タイムアウトを超過する可能性がありました。ラボの Plugin タイムアウトを `120s` に拡大した後、action 3 のロールバックが完了し、ジャーナルに `rolledBack` が記録されました。

最終 teardown はオプション B を使用: 既存の SAM ラボ状態を復元。

- `10.77.60.9` セカンダリプライベート IP が再び存在。
- `skipSourceDestCheck=true`。
- `routerd-cloud-oci`: `STOPPED`。
- `oci-cloud-client`: `STOPPED`。

コスト状態:

- OCI コンピューティング停止済み。
- 既存のパブリック IP、ブートボリューム、VNIC、サブネット、VCN、ポリシーは再利用可能な SAM ラボ状態として残存。

## ノート

- OCI Ubuntu イメージにターミナルの iptables reject ルールがありました。OCI SAM スモークで使用したものと同じラボファイアウォールブートストラップをデータプレーン検証前に適用。
- 最初の executor 試行では、インスタンスプリンシパルポリシーがプライベート IP 作成に対して狭すぎることが判明。ラボのダイナミックグループポリシーを拡大した後、retry2 のアクションペアがパス。
- 最初の通常ユーザーでのロールバック試行は、アクション DB のファイルパーミッションにより拒否されました。ロールバックは `sudo routerctl` で実行し、アクション DB の所有権と一致させました。
