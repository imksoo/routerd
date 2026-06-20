---
title: プラグインプロトコル
slug: /reference/plugin-protocol
---

# プラグインプロトコル

routerd のプラグインは、信頼済みのローカル実行ファイルです。
本体に組み込まないリソース固有の処理を、同じホスト上の小さなプログラムとして追加するための仕組みです。

リモートからのプラグイン登録、リモートインストール、公開レジストリは、意図的に対象外です。

## 配置

標準の配置先は次の通りです。

```text
/usr/local/libexec/routerd/plugins/<name>/
```

各プラグインはマニフェストと実行ファイルを持ちます。

```text
plugin.yaml
bin/<plugin>
```

## 役割

プラグインは次のような処理を担当できます。

- リソースの検証
- 変更計画の作成
- ホスト状態の観測
- ホストへの適用

ただし、ネットワーク状態を変更する処理は、テストしやすい小さな単位に分けます。
本体と同じく、ホストネットワークを変更するテストは、`tests/netns` などの隔離環境で行います。

## MVP のポリシー

CloudEdge MVP のプラグインは、信頼済みのローカル実行ファイルだけを対象にします。
routerd はリモートレジストリからの取得もリモートインストールも行いません。

プラグインの出力は、dynamic-config への保存や effective-config の導出に使う前に、必ず検証されます。
プラグインはリソース、ディレクティブ、プロバイダー action plan、イベントを提案できます。
`actionPlans` は dynamic-config の中では不活性であり、プラグインランナーやマージパスで実行されることはありません。
provider-action journal にインポートし、`ProviderActionPolicy`、承認、許可リスト、dry-run/live mode のゲートを通過した場合にのみ、エグゼキュータープラグインに渡せます。

![信頼済みローカルプラグインの観測が DynamicConfigPart へ入り、不活性なプロバイダー action plan は別経路でゲート付き action journal とエグゼキュータープラグインのパスへ進む dynamic config 図](/img/diagrams/dynamic-config-provider-actions.png)

## リソース形状

プラグインは、ローカル実行ファイルと省略可のトリガーセットとして宣言します。

```yaml
apiVersion: plugin.routerd.net/v1alpha1
kind: Plugin
metadata:
  name: oci-inventory
spec:
  executable: /usr/local/libexec/routerd/plugins/oci-inventory/bin/oci-inventory
  timeout: 10s
  capabilities: [observe.cloud, propose.dynamicConfig]
  triggers:
    - type: interval
      every: 300s
    - type: event
      topic: routerd.cloud.oci.refresh
```

dynamic config ソースは、プラグインを dynamic-config 生成ポリシーにバインドします。

```yaml
apiVersion: plugin.routerd.net/v1alpha1
kind: DynamicConfigSource
metadata:
  name: oci-inventory
spec:
  pluginRef: oci-inventory
  ttl: 300s
  mergePolicy:
    conflict: reject
```

ランナーは `spec.executable` が絶対パスの実行可能ファイルであることを要求します。
利用できる capability は `observe.cloud`、`observe.providerPrivateIPs`、`propose.dynamicConfig`、`propose.providerAction`、`execute.providerAction` です。
interval トリガーは `every` を、event トリガーは `topic` を使います。

## トリガー

プラグインは明示的なトリガーで実行されます。

| トリガー | 意味 |
| --- | --- |
| `interval` | 定期的な更新。インベントリやリース的な観測に使います。 |
| `event` | イベントバス駆動の更新。名前付きトピックで発火します。 |

`PluginRequest.spec.trigger` フィールドに、その呼び出しの実際のトリガーが記録されます。
`trigger.type` は `interval` または `event` で、`trigger.topic` はイベントトリガーの場合に設定されます。

## 入出力の契約

routerd はプラグインの実行ファイルを起動し、標準入力に `PluginRequest` の JSON オブジェクトを 1 つ書き込み、標準出力から `PluginResult` の JSON オブジェクトを 1 つ読み取ります。
タイムスタンプは RFC3339 形式です。
duration 文字列は `300s` のような Go 形式の構文です。

子プロセスが受け取る環境変数は最小限です。
routerd 自身の環境からの `PATH`（未設定なら固定のシステムフォールバック）と、`Plugin.spec.env` で明示した項目だけです。
routerd は親プロセスの環境変数を丸ごと引き継ぎません。

### PluginRequest

```json
{
  "apiVersion": "plugin.routerd.net/v1alpha1",
  "kind": "PluginRequest",
  "metadata": {
    "name": "oci-inventory"
  },
  "spec": {
    "trigger": {
      "type": "interval",
      "topic": ""
    },
    "startupConfigHash": "sha256:...",
    "effectiveGeneration": 44,
    "previousDynamicGeneration": 12,
    "now": "2026-05-29T12:00:00Z"
  }
}
```

| フィールド | 意味 |
| --- | --- |
| `spec.trigger` | このプラグイン呼び出しが発生した理由。 |
| `spec.startupConfigHash` | 現在の startup-config のダイジェスト。 |
| `spec.effectiveGeneration` | この結果の適用前の effective-config の世代番号。 |
| `spec.previousDynamicGeneration` | このソースで最後に受理された世代番号。 |
| `spec.now` | routerd が呼び出した時刻。 |

### PluginResult

```json
{
  "apiVersion": "plugin.routerd.net/v1alpha1",
  "kind": "PluginResult",
  "metadata": {
    "name": "oci-inventory"
  },
  "status": {
    "observedAt": "2026-05-29T12:00:00Z",
    "ttl": "300s",
    "resources": [
      {
        "apiVersion": "hybrid.routerd.net/v1alpha1",
        "kind": "RemoteAddressClaim",
        "metadata": { "name": "app-10-0-1-123" },
        "spec": {
          "domainRef": "cloudedge-same-subnet",
          "address": "10.0.1.123/32",
          "ownerSide": "cloud",
          "capture": {
            "type": "provider-secondary-ip",
            "providerRef": "oci-prod",
            "providerMode": "vnic-private-ip",
            "nicRef": "ocid1.vnic.oc1..example"
          },
          "delivery": {
            "peerRef": "cloud-main",
            "mode": "route",
            "tunnelInterface": "wg-hybrid"
          }
        }
      }
    ],
    "directives": [
      {
        "op": "mask",
        "target": {
          "apiVersion": "net.routerd.net/v1alpha1",
          "kind": "IPv4Route",
          "name": "cloud-app-static-fallback"
        },
        "reason": "RemoteAddressClaim/app-10-0-1-123 is active"
      }
    ],
    "actionPlans": [
      {
        "name": "assign-cloud-secondary-ip",
        "provider": "oci",
        "action": "assign-secondary-ip",
        "target": {
          "nicRef": "ocid1.vnic.oc1..example",
          "address": "10.0.1.123"
        },
        "undo": {
          "action": "unassign-secondary-ip"
        }
      }
    ],
    "events": [
      {
        "type": "InventoryObserved",
        "message": "observed app private address",
        "attributes": {
          "provider": "oci",
          "address": "10.0.1.123"
        }
      }
    ]
  }
}
```

routerd はプラグインの標準出力を YAML デコーダーで読み取ります（プラグインが JSON を出力した場合でも同様です）。
リソースの spec が routerd の型付き構造体に復元されます。
routerd は結果の形状を検証し、受理した出力を `DynamicConfigPart` として保存し、`observedAt + ttl` から `expiresAt` を導出します。
dynamic override policy の評価を含む完全な effective-config の検証は、dynamic part が startup config とマージされるときに行われます。

`actionPlans` は、運用者が provider-action journal にインポートすることを選べるプロバイダー操作を記述します。
プラグインの結果そのものはドライランの計画に留まる必要があり、`mode: execute` は拒否されます。
実際のプロバイダー変更は、`routerctl action execute --approved` またはデーモンの自動実行ゲート経由でのみ行われます。
エグゼキュータープラグインは routerd が保持する秘密を一切受け取りません。

### ObservePrivateIPsResult

`observe.providerPrivateIPs` capability を持つプラグインは `providerinventory.routerd.net/v1alpha1` の `ObservePrivateIPsResult` を返します。
従来の `status.ips` は wire 互換のため残り、ownership-discovery event を発行する候補アドレスとして扱われます。
新しいプラグインは、routerd が trap 除外や ownership selector を適用する前の、スキャン対象 VPC/VNet/VCN または subnet にある VM NIC と Private Endpoint のアドレス一覧を `status.localIPs` にも入れてください。
`localIPs` がない場合、routerd は `observedCandidates`、次に `ips` へフォールバックします。

プラグインが完全な local inventory を `localIPs` で返しつつ、event 発行候補だけを狭めたい場合は `status.observedCandidates` を使えます。
SAM の ownership resolver は shadow locality 分類に `localIPs` を使い、既存の discovery event 経路は `observedCandidates` または legacy `ips` を使い続けます。

各 private IP record は、プロバイダーが取得できる場合は `resourceRef` にその IP が紐づく compute instance ID を入れ、`resourceType` で router NIC、通常の instance NIC、Private Endpoint などを区別してください。
`status.self` もローカル router instance の `resourceRef` / `resourceType` を設定できます。
SAM はこの identity を使って、router instance に capture された secondary IP と、同じ provider subnet 上の本来の home client address を区別します。

## CLI

MVP の運用者向けコマンドは次の通りです。

```text
routerctl plugin list [--config <startup>] [-o table|json|yaml]
routerctl plugin run <name> [--dry-run] [--config <startup>] [--state-file <db>] [-o table|json|yaml]
routerctl action import|list|show|approve|execute|journal|rollback ...
```

`plugin run --dry-run` はプラグインを実行し、候補の `DynamicConfigPart` を表示しますが、状態 DB には書き込みません。
`--dry-run` なしの場合、routerctl はプラグインの実行を記録し、検証済みの dynamic part をローカルの状態データベースに保存します。

## 現在の位置付け

routerd の主要なルーター機能は、本体のバイナリと専用デーモンで実装を進めています。
プラグインプロトコルは、利用者ごとのローカル拡張を安全に取り込むための基盤です。
マニフェストの形式と入出力の契約は、安定した公開インターフェースとして固定するまでに変更される可能性があります。

[ハイブリッドクラウドエッジ設計](/docs/design-hybrid-cloud-edge) および [Dynamic config リファレンス](./reference/dynamic-config.md) も参照してください。
