# SAMSubnetPolicy

[リソース別の設定](../resources.md#samsubnetpolicy) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: mobility.routerd.net/v1alpha1` · `metadata.name`

シャード割当を連携イベントとして公開し、各ノードのMobilityPool所有権探索を担当IPv4サブネットに絞ります。探索範囲の割当であり、IP経路を直接設定するリソースではありません。

## 指定条件と参照関係

- sourcePrefixと全shardはIPv4 CIDRが必要です。各shardはsourcePrefix内で重複・包含不可ですが、元prefix全体を覆う必要はありません。各shardには1件以上の空でない重複しない割当ノードが必要です。
- 割当イベントは10分で失効し、再調整で更新します。割当欠落・失効・イベントストア読取エラーでは全拒否ではなくシャード絞り込みなしになります。これだけを認可境界に使わないでください。

## 設定構文

説明用の構文表記であり、そのままコピーするYAMLではありません。囲みのない項目は親の中で必須、`[ ... ]`は条件に応じて省略できる項目、`<型>`は差し替え値、`(a | b)`は選択肢です。任意の親にも必須の子があります。条件付き必須・排他・実効既定値は下の表を確認し、通常のYAMLはリンク先の設定例を使ってください。

```text
- apiVersion: mobility.routerd.net/v1alpha1
  kind: SAMSubnetPolicy
  metadata:
    name: "<name>"
  spec:
    groupRef: <string>
    poolRef: <string>
    shards:
      -
        assignedNodes:
          - <string>
        prefix: <string>
    sourcePrefix: <string>
```

## 項目の意味と既定値

任意の親の必須項目は、親を使う場合に必要です。それ以外の具体的な条件は説明欄に記載します。`[ ... ]`で囲まれた選択肢も、いずれか一方が必要な場合があります。

| YAML | 指定要否 | 型・制約 | 項目の意味と既定値 |
| --- | --- | --- | --- |
| `spec.sourcePrefix` | **必須** | string | 全shardの範囲を規定する必須IPv4 CIDRで、既定値はありません。包含検証に使い、アドレス・経路として適用しません。 |
| `spec.poolRef` | **必須** | string | イベントペイロードの照合キーとなる必須MobilityPool名です。Kind/nameではありません。現行検証は非空のみでプール存在までは確認しないため、受信するプールの名前に一致させます。 |
| `spec.groupRef` | **必須** | string | 割当の発行・読取先の必須EventGroup参照です。実行時にグループ解決が必要で、形の検証では非空のみを確認します。対象MobilityPool.groupRefと同じ表記にします。 |
| `spec.shards` | **必須** | array | 重複しないサブネットと担当nodeRefの必須非空一覧です。同じノードが別shardに属することは可能です。 |
| `spec.shards[].prefix` | **必須** | string | sourcePrefix内の必須IPv4 CIDRです。sourcePrefixと同じ長さの全体割当も可能ですが、shard間の重複・包含は拒否します。 |
| `spec.shards[].assignedNodes` | **必須** | array | 必須の非空nodeRef一覧で、同一shard内で重複不可です。現行検証はSAMNodeSet所属までは確認しないため、受信側EventGroupのローカルノード識別子と一致させます。 |

実装: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_mobility.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_mobility.go) · [pkg/controller/mobility/shard_controller.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/mobility/shard_controller.go) · [pkg/controller/mobility/discovery.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/mobility/discovery.go)

## 状態値の参照に使えるフィールド

通常の状態値参照の検証で、このKindについて許可するフィールド名です。許可一覧にあるだけでは値の生成・取得を保証しないため、実際の状態を確認してください。YAMLのspecに設定する項目ではなく出力です。

このKindには通常の状態値参照の許可フィールドが登録されていません。
