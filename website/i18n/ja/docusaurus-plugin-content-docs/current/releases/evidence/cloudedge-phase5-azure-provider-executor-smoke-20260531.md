# CloudEdge Phase 5.1 Azure Provider Executor スモーク

Result: PASS

日付: 2026-05-31 UTC  
ブランチ/ビルド: `phase5-oci-azure-executors` / `routerd v20260528.2308 (c51ba0ca)`  
エビデンスバンドル: `/home/imksoo/routerd-labs/cloudedge-sam/evidence/20260531T013055Z-phase5-azure-live-c51ba0ca`

## スコープ

- プロバイダーミューテーション対象: Azure のみ。
- テナント/サブスクリプション/リージョン: `53a7de65-6b1f-4878-a424-acad5e25db4b` / `26412fa4-cd3a-4128-9794-72ee01876d84` / `japaneast`。
- 再利用した routerd 専用 SAM ラボ: リソースグループ `cloudedge-lab`。
- 対象ルーター VM: `routerd-cloud`、プライベート `10.77.60.4`、パブリック `20.46.113.237`。
- 対象クライアント VM: `cloud-client`、プライベート `10.77.60.7`。
- 対象 NIC: `ce-router-nic`。
- キャプチャアドレス: `10.77.60.9`。

## リベースライン

ミューテーション前に、既存の Azure SAM ラボをフレッシュなプロバイダーベースラインにリセット:

- `ce-router-nic` からセカンダリ ipconfig `ipconfig-onprem-capture` / `10.77.60.9` を削除。
- `ce-router-nic` で `enableIPForwarding=false` を復元。
- リセット後エビデンス: `azure-router-nic-post-reset.json`、`post-reset-nic-summary.tsv`。

リセット後の状態:

- `ce-router-nic`: `ipForwarding=false`。
- IP configs: プライマリ `10.77.60.4` のみ。

## マネージド ID ゲート

`routerd-cloud` がシステム割り当てマネージド ID を受領:

- プリンシパル ID: `4b9423bc-01e3-4244-a898-b911f140cb6f`。
- executor 用に Azure CLI を `routerd-cloud` にインストール。
- ルーターからのマネージド ID preflight がパス:
  - `az login --identity --allow-no-subscriptions`
  - `az network nic show --ids <ce-router-nic>`

初期の NIC スコープ Network Contributor ロールは `ip-config create` に不十分でした。Azure が関連 NSG の `join/action` 権限も要求したためです。進行優先の修正として、ラボリソースグループと NSG のスコープに Network Contributor を追加。その後、executor のミューテーションが成功。

## Executor 実行

`azure-provider-executor` を `routerd-cloud` にビルドしインストール。

ルーター設定に以下を含む:

- `ProviderActionPolicy/azure-live-mutation`
- `Plugin/azure-executor`
- Plugin timeout `120s`
- `AZURE_CONFIG_DIR=/var/lib/routerd/azure`

アクション実行:

- `ensure-forwarding-enabled`
  - Action ID: `4`
  - Result: `succeeded`
  - 観測されたジャーナルファクト: `priorIpForwarding=false`
  - 結果メッセージ: `set ipForwarding=true`
- `assign-secondary-ip`
  - Action ID: `7`
  - Result: `succeeded`
  - 結果メッセージ: `assigned 10.77.60.9 to ce-router-nic (ip-config ipconfig-onprem-capture)`

ミューテーション後の Azure 検証:

- `ce-router-nic`: `ipForwarding=true`。
- IP configs: `10.77.60.4`、`10.77.60.9`。
- エビデンス: `azure-router-nic-after-mutation.json`、`azure-router-nic-after-mutation-summary.tsv`。

## データプレーン検証

クラウド側:

- `routerctl doctor hybrid`: `overall=pass`、`warn=0`、`fail=0`、`skip=1`。
- 配送ルート: `10.77.60.9 dev wg-hybrid metric 120`。
- ローカル OS アドレス不在: `10.77.60.9/32 absent from local interfaces`。
- MSS clamp: `routerd_mss covers eth0 -> wg-hybrid`。

オンプレミス側:

- router06 `routerctl doctor hybrid`: `overall=pass`、`warn=0`、`fail=0`、`skip=1`。
- クラウドクライアント `10.77.60.7` の Proxy ARP claim は健全なまま。
- MSS clamp: `routerd_mss covers ens21 -> wg-hybrid`。

クライアント接続性:

- cloud-client `10.77.60.7` -> onprem-client `10.77.60.9` ping: `3/3`、`0% packet loss`。
- onprem-client `10.77.60.9` -> cloud-client `10.77.60.7` ping: `3/3`、`0% packet loss`。
- cloud -> onprem SSH ソース保持:
  - `SSH_CONNECTION=10.77.60.7 ... 10.77.60.9 22`
- onprem -> cloud SSH ソース保持:
  - `SSH_CONNECTION=10.77.60.9 ... 10.77.60.7 22`
- デフォルトゲートウェイ変更なし:
  - cloud-client: `default via 10.77.60.1 dev eth0`
  - onprem-client: `default via 10.77.60.1 dev eth0`
- NAT: SSH ソース保持により不在を確認。

## ロールバックとリストア

`routerctl action rollback` によるロールバックを実施:

- action 7 `assign-secondary-ip`: `rolledBack`、`ipconfig-onprem-capture` を割当解除。
- action 4 `ensure-forwarding-enabled`: `rolledBack`、`ipForwarding=false` を復元。

ロールバック中に修正可能なラボの問題が 1 件発見: ルーター設定の再適用後に Plugin 環境が `AZURE_CONFIG_DIR` を公開しなくなり、Azure CLI が `Please run 'az login'` を報告。設定を修正し、`/var/lib/routerd/azure` 配下でマネージド ID ログインを再作成した後、ロールバックがパス。

最終 teardown はオプション B を使用: 既存の Azure SAM ラボ状態を復元。

- `10.77.60.9` セカンダリ ipconfig が再び存在。
- `ipForwarding=true`。
- `routerd-cloud`: `VM deallocated`。
- `cloud-client`: `VM deallocated`。

コスト状態:

- Azure コンピューティング deallocated。
- 既存のパブリック IP、NIC、ディスク、VNet、NSG、マネージド ID/ロール割り当ては再利用可能な SAM ラボ状態として残存。

## ノート

- クラウドの `RemoteAddressClaim` ラボ設定に `capture.interface: eth0` を追加。新しい MSS/PMTU doctor チェックが `eth0 -> wg-hybrid` のカバレッジを証明できるようにするため。
- 初回のアクション試行はマネージド ID のロールスコープが狭すぎたため失敗。最終的に成功したアクションは ID 4 と 7。
- `rtk` ラッパーは長い Azure リソース ID をコマンド置換で切り詰めます。正確なリソース ID が必要なコマンドは `rtk bash -lc` 内で raw `az` を使用。
