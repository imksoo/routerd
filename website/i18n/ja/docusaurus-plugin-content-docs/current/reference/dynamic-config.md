---
title: Dynamic config
slug: /reference/dynamic-config
---

# Dynamic config

**Dynamic config** は、信頼されたローカルソースが startup-config を編集せずにランタイムの意図を提供する仕組みです。
routerd は startup YAML、アクティブな「dynamic part」、アクティブな「mask」から一つの **effective-config** を導出します。
「effective-config」が唯一のリコンサイル対象です。

CloudEdge MVP 向けの dynamic-config API の形状を以下に示します。
プラグインランナーは検証済みのプラグイン出力を **`DynamicConfigPart`** レコードとして保存できます。
`actionPlans` 由来のプロバイダー action は「dynamic config」内で不活性のまま保持され、「effective config」にはマージされません。
別の provider-action エンジンがアクティブな part からのみインポートし、`ProviderActionPolicy`、承認、エグゼキュータープラグインのゲートを経てのみ実行します。

![startup config、DynamicOverridePolicy、信頼されたローカルプラグイン出力、DynamicConfigPart、effective config、不活性 actionPlans、action journal、ゲート付きエグゼキュータープラグインのパスを示す Dynamic config の図](/img/diagrams/dynamic-config-provider-actions.png)

## DynamicConfigPart

「DynamicConfigPart」は、動的ソースからの検証済みランタイムフラグメントです。
ソースは通常の `api.Resource` オブジェクトとディレクティブを提供できます。

```yaml
apiVersion: config.routerd.net/v1alpha1
kind: DynamicConfigPart
metadata:
  name: oci-inventory
spec:
  source: Plugin/oci-inventory
  generation: 12
  observedAt: "2026-05-29T12:00:00Z"
  expiresAt: "2026-05-29T12:05:00Z"
  digest: sha256:...
  resources:
    - apiVersion: hybrid.routerd.net/v1alpha1
      kind: RemoteAddressClaim
      metadata: { name: app-10-0-1-123 }
      spec:
        domainRef: cloudedge-same-subnet
        address: 10.0.1.123/32
        ownerSide: cloud
        capture: { type: provider-secondary-ip, providerRef: oci-prod, providerMode: vnic-private-ip, nicRef: ocid1.vnic.oc1..example }
        delivery: { peerRef: cloud-main, mode: route, tunnelInterface: wg-hybrid }
  directives:
    - op: mask
      target: { apiVersion: net.routerd.net/v1alpha1, kind: IPv4Route, name: cloud-app-static-fallback }
      reason: "RemoteAddressClaim/app-10-0-1-123 is active"
```

| フィールド | 意味 |
| --- | --- |
| `spec.source` | 安定したソース識別子。例: `Plugin/oci-inventory`。 |
| `spec.generation` | 単調増加のソース世代番号。説明と順序付けに使用。 |
| `spec.observedAt` | ソースが入力を観測した RFC3339 時刻。 |
| `spec.expiresAt` | この part が非アクティブになる RFC3339 時刻。 |
| `spec.digest` | 検証済み part ペイロードのダイジェスト。 |
| `spec.resources` | アクティブな間「effective-config」に提供されるリソース。 |
| `spec.directives` | マージディレクティブ。現在は `op: mask` のみ。 |
| `spec.actionPlans` | プロバイダー action の提案(リソースではない)。provider-action エンジンがアクティブな part からのみインポートし、実行前に独自のゲートを適用する。 |

プラグインは `PluginResult.status.ttl` で TTL duration を返します。
routerd はそれを `observedAt` に対して解決し、保存される `expiresAt` にします。

## DynamicConfigSource

**`DynamicConfigSource`** は、一つのプラグインを「dynamic config」生成にバインドする startup-config ポリシーです。

```yaml
apiVersion: plugin.routerd.net/v1alpha1
kind: DynamicConfigSource
metadata: { name: oci-inventory }
spec:
  pluginRef: oci-inventory
  ttl: 300s
  mergePolicy:
    conflict: reject
```

MVP のマージポリシーは `conflict: reject` のみをサポートします。

## DynamicConfigDirective

MVP は以下のディレクティブ操作をサポートします。

| 操作 | 意味 |
| --- | --- |
| `mask` | ディレクティブがアクティブな間、一致する startup-config リソースを抑制する。 |

ディレクティブのターゲットは `apiVersion`、`kind`、`name` で識別します。
ターゲットは完全一致です。
ワイルドカード「mask」は MVP のスコープ外です。

## DynamicOverridePolicy

**`DynamicOverridePolicy`** は、ソースが選択したリソースに対して動的ディレクティブを使用する権限を付与します。
プラグインは「mask」を提案できますが、「mask」がアクティブになるのはポリシーがそのソース、操作、ターゲットを許可している場合のみです。

```yaml
apiVersion: config.routerd.net/v1alpha1
kind: DynamicOverridePolicy
metadata: { name: allow-cloud-plugin-mask }
spec:
  allow:
    - source: Plugin/oci-inventory
      operations: [mask]
      targets:
        - { apiVersion: net.routerd.net/v1alpha1, kind: IPv4Route, name: cloud-app-static-fallback }
```

ポリシーは startup-config の意図です。
動的ソースが自身にオーバーライド権限を付与することはできません。

## マージアルゴリズム

「effective-config」のマージは決定的です。

1. startup-config を読み込み検証。
2. 状態データベースから検証済み「dynamic part」を読み込み。
3. `expiresAt` がマージ時刻以前の「dynamic part」を破棄。
4. アクティブな「dynamic part」を `source`、次に `generation`、次に `metadata.name` でソートし、安定したレンダリングと診断を実現。
5. アクティブなディレクティブを「DynamicOverridePolicy」に照合して評価。
6. 許可されたアクティブ「mask」の対象となる startup リソースを抑制済みとしてマーク。
7. 抑制されていない startup リソースとアクティブな dynamic リソースから「effective-config」を構築。
8. リコンサイルまたはドライラン出力の前に、結果の「effective-config」を検証。

競合ルール:

- 動的リソースは、同じ `apiVersion`、`kind`、`metadata.name` の startup リソースを置き換えてはならない。
- 同一の識別子を持つ 2 つのアクティブな動的リソースは、ソース固有の所有権ルールが後に定義されない限り競合する。
- 不許可のディレクティブはマージでは無視され、検証または診断の所見として報告される。
- 期限切れの「dynamic part」はリソースも「mask」も提供しない。

## mask のセマンティクス(抑制と復元)

「mask」は抑制であり、削除ではありません。
startup YAML は変更されず、git 履歴は運用者が所有したままです。
一致するすべてのアクティブ「mask」が期限切れまたは削除されると、静的リソースが再びアクティブになります。

抑制されたリソースは以下のステータスを表示します。

```yaml
status:
  phase: Suppressed
  maskedBy:
    - Plugin/oci-inventory#12
  maskedUntil: "2026-05-29T12:05:00Z"
```

複数の「mask」が同じリソースを対象とする場合、最後のアクティブ「mask」が期限切れになるまでリソースは抑制され続けます。
`maskedBy` はすべてのアクティブなソースと世代番号をリストし、`maskedUntil` はアクティブ「mask」のうち最も遅い `expiresAt` です。

MVP の期限切れ動作は `onExpire=restoreStatic` です。
「mask」が期限切れになると、routerd は次のマージで startup-config リソースを「effective-config」に復元します。
startup リソースが変更されていないため、破壊的なクリーンアップステップはありません。

## routerctl dynamic サブコマンド

現在の運用者向けインターフェースです。

```text
routerctl dynamic list
routerctl dynamic describe <source-or-part>
routerctl dynamic render
routerctl dynamic diff
routerctl plugin list
routerctl plugin run <name> [--dry-run]
```

`dynamic list` はアクティブおよび期限切れの part を表示します。
`dynamic describe` はソース、世代番号、ダイジェスト、リソース、ディレクティブ、有効期限を出力します。
`dynamic render` は「effective-config」を出力します。
`dynamic diff` は startup-config と「effective-config」を比較します。
`plugin run --dry-run` は状態データベースに書き込まずに候補の「dynamic part」を出力します。

[ハイブリッドクラウドエッジ設計](../design-hybrid-cloud-edge) および
[プラグインプロトコル](/docs/reference/plugin-protocol) を参照してください。
