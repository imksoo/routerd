---
title: CloudEdge 自律ラボ (cloudedge-labctl)
---

# CloudEdge 自律ラボ (`cloudedge-labctl`)

![cloudedge-labctl のラボライフサイクル、スモークとフェイルオーバーアクション、エビデンス収集、dry-run デフォルト、TTL タグ、teardown ガードの流れ](/img/diagrams/how-to-cloudedge-autonomous-lab.png)

> 実験的機能（CloudEdge）。エージェントが人間の Runbook 確認なしに CloudEdge **Selective Address Mobility (SAM)** フェイルオーバーラボを実行できる、単一コマンドのハーネスです。インターフェースを固定し、クラウド以外のロジック（run-id とタグ規約、TTL と teardown コストガード、障害プリミティブ、接続マトリクス、エビデンス組み立て）をすべて実装しています。実際のプロバイダーごとのプロビジョニングは、既存の [`examples/cloudedge-mobility-demo/`](https://github.com/imksoo/routerd/tree/main/examples/cloudedge-mobility-demo) パッケージをラップするか、Terraform と CLI の連携用に `TODO(lab-operator)` としてマークされています。

ハーネスは `scripts/cloudedge-labctl.sh` で、2 つのヘルパーがあります:

- `scripts/cloudedge-connectivity-matrix.sh`：方向付き ping と ssh のマトリクスおよびアサーション。
- `scripts/cloudedge-evidence-schema.json`：ランエビデンスの JSON スキーマ。

`--help`、dry パス、`down --expired` には**クラウド認証情報は不要**です。

## ライフサイクル

```sh
scripts/cloudedge-labctl.sh up        --profile full --provider aws,oci,azure,onprem --ttl 4h
scripts/cloudedge-labctl.sh deploy    --commit HEAD          # または --build <dist path>
scripts/cloudedge-labctl.sh smoke     --matrix d3 --out /tmp/matrix.json
scripts/cloudedge-labctl.sh failover  --provider aws --fault stop-active
scripts/cloudedge-labctl.sh smoke     --matrix d3 --out /tmp/matrix-after.json
scripts/cloudedge-labctl.sh evidence  collect --out evidence/<run-id> --matrix-json /tmp/matrix-after.json
scripts/cloudedge-labctl.sh down      --run-id <run-id> --force
```

`up` は **run-id** を stdout に出力します。これをキャプチャして後続のコマンドに渡してください。クラウドへの変更操作は**デフォルトで DRY**（`CE_DRY_RUN=1`）です。認証情報と予算の承認後に `CE_DRY_RUN=0` を設定して実際に実行します。

## プロファイル

| プロファイル | サイト数 | 用途 |
| --- | --- | --- |
| `minimal` | on-prem とクラウド 1 | 最安のスモーク、インターフェースと CI の検証 |
| `provider` | 1 プロバイダーの A/B ルーターとクライアント | プロバイダーパリティ（AWS、OCI、Azure の seize） |
| `full` | on-prem、AWS、OCI、Azure | 4 サイト `/24` 12 フローデモ |
| `soak` | TTL 全期間稼働させる `full` ラン | 長時間の再収束チェック |

`soak` は運用上は長い `--ttl` で維持する `full` ランです（TTL まで `down` を実行しないでください）。BFD と BGP の再収束の検証に使います。

## TTL とコストポリシー

すべてのクラウドリソースは以下のタグを**必ず**付与します（ヘルパー `cloudedge_tags()` が出力し、`up` がスタンプします）:

```text
routerd.cloudedge.run_id          <UTCdate>T<time>-cloudedge-<scenario>
routerd.cloudedge.owner
routerd.cloudedge.ttl_expires_at  絶対 UTC RFC3339
routerd.cloudedge.provider
routerd.cloudedge.purpose
```

コストガードルール:

- `up --ttl <dur>` で `ttl_expires_at` をスタンプします。ランに適した最短の TTL を選んでください。
- `down --run-id <id>` は 1 つのランを teardown します。`down --expired` は TTL を過ぎた**すべて**のランを teardown します（ラボがない場合は安全に no-op で exit 0）。
- `up` は `--ttl` を事前に検証し、不正な期間では**ハードフェイル**（非ゼロ終了）します。壊れた/すでに期限切れのコストガードでラボが起動されることはありません。
- ハーネスの **EXIT トラップ**は、`up` が中断されたり、プロバイダーが**起動途中で**失敗した場合にランを teardown します（進行中の期間のみ有効化され、正常完了時に解除されます。正常な `up` はラボを明示的な `down` または TTL まで存続させます）。`up --keep` を指定すると、調査用に部分状態を残します。
- 失敗時も含め、毎回のラン後に必ず `down`（または janitor からの `down --expired`）を実行してください。TTL 超過のラボは run-id なしでクリーンアップ可能です。

## 障害プリミティブ (`failover --fault`)

| 障害 | 意味 | 初期配線 |
| --- | --- | --- |
| `stop-active` | アクティブルーター VM/インスタンスを停止 | プロバイダー stop CLI（`reset-lab.sh` 参照） |
| `drain` | アクティブの MobilityPool で `maintenance.drain=true` | `run-demo.sh` の `*-drain.yaml` を再利用 |
| `routerd-bgp-stop` | `routerd-bgp` を停止（BGP セッション切断） | ssh `systemctl stop routerd-bgp` |
| `executor-fail` | プロバイダーアクション executor 拒否（識別情報のスコープダウン） | 識別情報ポリシー |
| `stale-replay` | 古い pathSig アクションをリプレイ。**フェンスされる**必要あり | `probe_stale_gate_on_aws_b` |

障害を注入してから `smoke` と `evidence collect` を再実行し、復旧を証明します。

## エビデンススキーマ

`evidence collect --out <dir>` は `<dir>/result.json` を出力します。`scripts/cloudedge-evidence-schema.json` に対して検証され、`summary.md` と（指定されていれば）接続マトリクス JSON も出力されます。形式:

```json
{
  "runId": "20260601T031500Z-cloudedge-aws-failover",
  "commit": "<sha>",
  "scenario": "aws-active-stop-seize",
  "result": "pass",
  "providers": {
    "aws":    {"dataplane": "pass", "providerState": "pass"},
    "oci":    {"dataplane": "pass", "providerState": "pass"},
    "azure":  {"dataplane": "pass", "providerState": "pass"},
    "onprem": {"dataplane": "pass", "providerState": "pass"}
  },
  "assertions": [
    {"name": "ownership_epoch_bumped", "result": "pass"},
    {"name": "allow_reassignment_maintained_until_success", "result": "pass"},
    {"name": "source_ip_preserved", "result": "pass"},
    {"name": "default_gateway_unchanged", "result": "pass"},
    {"name": "old_holder_residue_absent", "result": "pass"},
    {"name": "stale_action_fenced", "result": "pass"}
  ],
  "costGuard": {"ttlHours": 4, "teardown": "completed"}
}
```

データプレーンのチェックと `source_ip_preserved` / `default_gateway_unchanged` は接続マトリクスから自動的に導出されます。seize/フェンシングアサーション（`ownership_epoch_bumped`、`allow_reassignment_maintained_until_success`、`old_holder_residue_absent`、`stale_action_fenced`）と `providerState` は最初 `na` で、ラボ運用者がプロバイダーインベントリ、BGP mobility パス、プロバイダー trap アクションプラン、アクションジャーナルから取り込みます（`collect-evidence.sh` 参照）。ランが **PASS** となるのは `result == pass` かつすべての必須アサーションがパスした場合のみです。

## 接続マトリクス

`cloudedge-connectivity-matrix.sh` は共有 `/24` 上のすべての方向付き `src -> dst` フローを実行し、フローごとに以下をアサートします:

- **source-IP-preserved** — 宛先がクライアントサイトの実際のソース IP（mobility `/32`）をピアアドレスとして認識する（NAT なし）。
- **default-gw-unchanged** — ソースクライアントのデフォルトゲートウェイが変更されていない。
- **no-NAT** — ping が宛先に到達し、SSH のピア IP がソース IP と一致する。

実行は `MATRIX_RUNNER` の間接指定を通じて行われるため、マトリクスは**オフラインで単体実行可能**です（`MATRIX_RUNNER` にスタブを設定）。実際のラボでは、デフォルトのランナーがデモ環境に対して `ssh`/`ping` を使います。出力はフローごとの JSON で、`evidence collect --matrix-json` で取り込めます。

## 自律性チャーター（概要）

エージェントは**ラボ起動、デプロイ、障害注入、データプレーン検証、エビデンス、teardown、issue と PR の更新**のフルループを所有し、人間が Runbook を読むことなく実行します。
クラウドアクションはデフォルトで dry であり、明示的な認証情報と予算の承認でゲートされています。
エージェントは常にラボを teardown 済みか TTL コストガード内の状態にしなければならず、PASS には必ずスキーマ有効なエビデンスバンドルを添付する必要があります。

## 人間のゲート

以下のみ人間が必要です。それ以外はすべて自動化されています:

1. **予算。** 支出の承認、TTL または予算上限の引き上げ。
2. **認証情報と権限。** クラウド認証情報と executor が使用する最小権限の識別情報やロールの付与（秘密はコミットされず、プラグインにも渡されません）。
3. **マージ。** PR の最終承認。
4. **本番。** 本番ロールアウト（ラボハーネスでは一切実行されません）。

## 注意事項

- これは**ラボハーネス**であり、本番用のターンキーソリューションではありません。
- 初期実装では、実際のプロバイダーごとの割り当て、teardown、ノードプッシュは `TODO(lab-operator)` のスタブか、デモパッケージの薄いラッパーです。run-id タグでフィルタリングした Terraform や OpenTofu、またはプロバイダー CLI を接続してください。
- 実際のアカウント ID、サブスクリプション ID、OCID、ENI や VNIC の ID、秘密情報、秘密鍵は決してコミットしないでください。`env.example` のようにプレースホルダーの論理アドレスを使用してください。
