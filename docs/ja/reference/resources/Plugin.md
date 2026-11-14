# Plugin

[リソース別の設定](../resources.md#plugin) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: plugin.routerd.net/v1alpha1` · `metadata.name`

routerdのプラグインプロトコルで実行する外部プログラムを登録します。明示的な実行、またはEventSubscriptionなどの利用側から呼び出します。

## 指定条件と参照関係

- executableは絶対パスが必須です。実行時には存在する通常ファイルであり、実行権限のビットが少なくとも1つあることを確認します。
- 現行実装ではtriggersの宣言だけで周期・イベント実行は開始しません。値の検証とplugin listでの表示はありますが、プラグインを呼び出す利用側が必要です。
- contextの参照はapiVersion・kind・nameの3項目で指定します。参照先が存在しなくても設定検証ではエラーにせず、実行時の渡し先一覧から省きます。秘密情報は秘匿化されます。

## 設定構文

説明用の構文表記であり、そのままコピーするYAMLではありません。囲みのない項目は親の中で必須、`[ ... ]`は条件に応じて省略できる項目、`<型>`は差し替え値、`(a | b)`は選択肢です。任意の親にも必須の子があります。条件付き必須・排他・実効既定値は下の表を確認し、通常のYAMLはリンク先の設定例を使ってください。

```text
- apiVersion: plugin.routerd.net/v1alpha1
  kind: Plugin
  metadata:
    name: "<name>"
  spec:
    [ capabilities:
        - ("observe.cloud" | "observe.providerPrivateIPs" | "propose.dynamicConfig" | "propose.providerAction" | "execute.providerAction")
    ]
    [ context:
        [ resources:
            -
              apiVersion: <string>
              kind: <string>
              name: <string>
        ]
    ]
    [ env:
        [ <name>: <string> ]
    ]
    executable: <string>
    [ timeout: <string> ]
    [ triggers:
        -
          [ every: <string> ]
          [ topic: <string> ]
          type: ("interval" | "event")
    ]
```

## 項目の意味と既定値

任意の親の必須項目は、親を使う場合に必要です。それ以外の具体的な条件は説明欄に記載します。`[ ... ]`で囲まれた選択肢も、いずれか一方が必要な場合があります。

| YAML | 指定要否 | 型・制約 | 項目の意味と既定値 |
| --- | --- | --- | --- |
| `spec.executable` | **必須** | string | 外部プログラムの絶対パス。必須で既定値はありません。 |
| `spec.timeout` | *任意* | string | 1回の実行時間の上限。省略時10s。指定する場合は30sなど、正のGo duration形式です。 |
| `spec.env` | *任意* | object | 環境変数名をキーにした追加変数。省略時は空です。runner自身のPATH、なければ組込PATHを用意してから上書きします。それ以外の親環境変数は継承しません。空のキーと=を含むキーは無視します。 |
| `spec.capabilities` | *任意* | array | 能力の宣言。省略時は空です。observe.cloud、observe.providerPrivateIPs、propose.dynamicConfig、propose.providerAction、execute.providerActionを受理します。インベントリ取得にはobserve.providerPrivateIPs、事業者操作の実行にはexecute.providerActionが必要です。この一覧自体は実行を開始しません。 |
| `spec.context` | *任意* | object | 標準入力に渡す設定リソースの許可一覧。省略時は空で、設定を渡しません。 |
| `spec.context.resources` | *任意* | array | 許可するリソース識別子の一覧。省略時は空です。ワイルドカード展開は行いません。 |
| `spec.context.resources[].apiVersion` | **条件付き必須** | string | 参照する設定リソースのAPIグループとバージョン。各要素で必須です。 |
| `spec.context.resources[].kind` | **条件付き必須** | string | 参照する設定リソースの種類。各要素で必須です。 |
| `spec.context.resources[].name` | **条件付き必須** | string | 参照する設定リソースのmetadata.name。各要素で必須です。 |
| `spec.triggers` | *任意* | array | トリガー宣言の一覧。省略時は空です。現行runtimeでは検証・表示だけで、動作条件は上記の注意事項を参照してください。 |
| `spec.triggers[].type` | **条件付き必須** | string; enum=["interval", "event"] | トリガーの区分。intervalかeventの指定が必須で、既定値はありません。 |
| `spec.triggers[].every` | *任意* | string | intervalでは必須の正のGo duration。eventではこの項目による周期実行はありません。既定値はありません。 |
| `spec.triggers[].topic` | *任意* | string | イベントトピックの宣言。任意で省略時は空です。eventでも検証上は必須になりません。リソース名ではありません。 |

実装: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_plugin.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_plugin.go) · [pkg/plugin/runner.go](https://github.com/imksoo/routerd/blob/main/pkg/plugin/runner.go) · [pkg/plugin/context.go](https://github.com/imksoo/routerd/blob/main/pkg/plugin/context.go) · [pkg/providerinventory/executor.go](https://github.com/imksoo/routerd/blob/main/pkg/providerinventory/executor.go) · [pkg/provideraction/executor.go](https://github.com/imksoo/routerd/blob/main/pkg/provideraction/executor.go)

## 状態値の参照に使えるフィールド

通常の状態値参照の検証で、このKindについて許可するフィールド名です。許可一覧にあるだけでは値の生成・取得を保証しないため、実際の状態を確認してください。YAMLのspecに設定する項目ではなく出力です。

このKindには通常の状態値参照の許可フィールドが登録されていません。
