# SAMSubnetPolicy

[リソース別の設定](../resources.md#samsubnetpolicy) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: mobility.routerd.net/v1alpha1` · `metadata.name`

シャード割当を連携イベントとして公開し、各ノードのMobilityPool所有権探索を担当IPv4サブネットに絞ります。探索範囲の割当であり、IP経路を直接設定するリソースではありません。

## 指定条件と参照関係

- sourcePrefixと全shardはIPv4 CIDRが必要です。各shardはsourcePrefix内で重複・包含不可ですが、元prefix全体を覆う必要はありません。各shardには1件以上の空でない重複しない割当ノードが必要です。
- 割当イベントは10分で失効し、再調整で更新します。割当欠落・失効・イベントストア読取エラーでは全拒否ではなくシャード絞り込みなしになります。これだけを認可境界に使わないでください。

## YAML定義

全フィールドを示す構造参照です。そのまま反映する設定例ではありません。プレースホルダーを実値に置換し、不要な項目を省き、両立する選択肢だけを使います。表示値は型を示す仮値であり、実効的な既定値と指定条件は下の項目説明に記載します。必須の表示はJSON Schemaに基づく親オブジェクト内での指定要否であり、意味上の検証では追加の項目や参照が必要になる場合があります。

```yaml
- apiVersion: mobility.routerd.net/v1alpha1
  kind: SAMSubnetPolicy
  metadata:
    name: "<name>"
  spec:
    groupRef: "<string>" # 必須; string
    poolRef: "<string>" # 必須; string
    shards: # 必須; array
      - # object
        assignedNodes: # 必須; array
          - "<string>" # string
        prefix: "<string>" # 必須; string
    sourcePrefix: "<string>" # 必須; string
```

## 項目の意味と既定値

| YAML | 項目の意味と既定値 |
| --- | --- |
| `spec.sourcePrefix` | 全shardの範囲を規定する必須IPv4 CIDRで、既定値はありません。包含検証に使い、アドレス・経路として適用しません。 |
| `spec.poolRef` | イベントペイロードの照合キーとなる必須MobilityPool名です。Kind/nameではありません。現行検証は非空のみでプール存在までは確認しないため、受信するプールの名前に一致させます。 |
| `spec.groupRef` | 割当の発行・読取先の必須EventGroup参照です。実行時にグループ解決が必要で、形の検証では非空のみを確認します。対象MobilityPool.groupRefと同じ表記にします。 |
| `spec.shards` | 重複しないサブネットと担当nodeRefの必須非空一覧です。同じノードが別shardに属することは可能です。 |
| `spec.shards[].prefix` | sourcePrefix内の必須IPv4 CIDRです。sourcePrefixと同じ長さの全体割当も可能ですが、shard間の重複・包含は拒否します。 |
| `spec.shards[].assignedNodes` | 必須の非空nodeRef一覧で、同一shard内で重複不可です。現行検証はSAMNodeSet所属までは確認しないため、受信側EventGroupのローカルノード識別子と一致させます。 |

実装: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_mobility.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_mobility.go) · [pkg/controller/mobility/shard_controller.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/mobility/shard_controller.go) · [pkg/controller/mobility/discovery.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/mobility/discovery.go)

## 状態値の参照に使えるフィールド

通常の状態値参照の検証で、このKindについて許可するフィールド名です。許可一覧にあるだけでは値の生成・取得を保証しないため、実際の状態を確認してください。YAMLのspecに設定する項目ではなく出力です。

このKindには通常の状態値参照の許可フィールドが登録されていません。
