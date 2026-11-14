# DynamicConfigSource

[リソース別の設定](../resources.md#dynamicconfigsource) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: plugin.routerd.net/v1alpha1` · `metadata.name`

プラグインを使う動的設定取得元の宣言です。現行実装には型の読込みと検証はありますが、DynamicConfigSourceSpecを消費するruntime処理はなく、宣言だけではプラグインを実行しません。

## 指定条件と参照関係

- pluginRefは名前の宣言で、検証時に参照先の存在確認は行いません。ここでttlは必須ですが、実際のプラグイン結果の統合は別途PluginResult.status.ttlを使います。このリソースで結果のTTLを上書きできるとは扱いません。

## 設定構文

説明用の構文表記であり、そのままコピーするYAMLではありません。囲みのない項目は親の中で必須、`[ ... ]`は条件に応じて省略できる項目、`<型>`は差し替え値、`(a | b)`は選択肢です。任意の親にも必須の子があります。条件付き必須・排他・実効既定値は下の表を確認し、通常のYAMLはリンク先の設定例を使ってください。

```text
- apiVersion: plugin.routerd.net/v1alpha1
  kind: DynamicConfigSource
  metadata:
    name: "<name>"
  spec:
    [ mergePolicy:
        [ conflict: ("" | "reject") ]
    ]
    pluginRef: <string>
    [ triggers:
        -
          [ every: <string> ]
          [ topic: <string> ]
          type: ("interval" | "event")
    ]
    ttl: <string>
```

## 項目の意味と既定値

任意の親の必須項目は、親を使う場合に必要です。それ以外の具体的な条件は説明欄に記載します。`[ ... ]`で囲まれた選択肢も、いずれか一方が必要な場合があります。

| YAML | 指定要否 | 型・制約 | 項目の意味と既定値 |
| --- | --- | --- | --- |
| `spec.pluginRef` | **必須** | string | 取得元として意図するPluginのmetadata.name。空白でない指定が必須で既定値はありません。Plugin/は付けません。現行ではこの宣言を実行時に解決する処理はありません。 |
| `spec.ttl` | **必須** | string | 正のGo durationが必須で既定値はありません。この宣言は検証されますが、runtime側からプラグインの実行や結果の期限に適用する処理はありません。 |
| `spec.mergePolicy` | *任意* | object | 任意の統合方針オブジェクト。現行ではこの取得元宣言を消費するruntime処理はありません。 |
| `spec.mergePolicy.conflict` | *任意* | string; enum=["", "reject"] | 検証を通るのは空値かrejectです。省略は受理しますが、いずれもこの宣言を介して既存リソースを置換する実行機能を有効にしません。 |
| `spec.triggers` | *任意* | array | トリガー宣言の一覧。省略時は空です。現行runtimeでは検証・表示だけで、動作条件は上記の注意事項を参照してください。 |
| `spec.triggers[].type` | **条件付き必須** | string; enum=["interval", "event"] | トリガーの区分。intervalかeventの指定が必須で、既定値はありません。 |
| `spec.triggers[].every` | *任意* | string | intervalでは必須の正のGo duration。eventではこの項目による周期実行はありません。既定値はありません。 |
| `spec.triggers[].topic` | *任意* | string | イベントトピックの宣言。任意で省略時は空です。eventでも検証上は必須になりません。リソース名ではありません。 |

実装: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/api/resource_kinds.go](https://github.com/imksoo/routerd/blob/main/pkg/api/resource_kinds.go) · [pkg/config/validate_plugin.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_plugin.go) · [pkg/plugin/dynamic_config.go](https://github.com/imksoo/routerd/blob/main/pkg/plugin/dynamic_config.go)

## 状態値の参照に使えるフィールド

通常の状態値参照の検証で、このKindについて許可するフィールド名です。許可一覧にあるだけでは値の生成・取得を保証しないため、実際の状態を確認してください。YAMLのspecに設定する項目ではなく出力です。

このKindには通常の状態値参照の許可フィールドが登録されていません。
