# Plugin

[リソース別の設定](../resources.md#plugin) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: plugin.routerd.net/v1alpha1` · `metadata.name`

routerdのプラグインプロトコルで実行する外部プログラムを登録します。明示的な実行、またはEventSubscriptionなどの利用側から呼び出します。

## 指定条件と参照関係

- executableは絶対パスが必須です。実行時には存在する通常ファイルであり、実行権限のビットが少なくとも1つあることを確認します。
- 現行実装ではtriggersの宣言だけで周期・イベント実行は開始しません。値の検証とplugin listでの表示はありますが、プラグインを呼び出す利用側が必要です。
- contextの参照はapiVersion・kind・nameの3項目で指定します。参照先が存在しなくても設定検証ではエラーにせず、実行時の渡し先一覧から省きます。秘密情報は秘匿化されます。

## YAML定義

全フィールドを示す構造参照です。そのまま反映する設定例ではありません。プレースホルダーを実値に置換し、不要な項目を省き、両立する選択肢だけを使います。表示値は型を示す仮値であり、実効的な既定値と指定条件は下の項目説明に記載します。必須の表示はJSON Schemaに基づく親オブジェクト内での指定要否であり、意味上の検証では追加の項目や参照が必要になる場合があります。

```yaml
- apiVersion: plugin.routerd.net/v1alpha1
  kind: Plugin
  metadata:
    name: "<name>"
  spec:
    capabilities: # 任意; array
      - "observe.cloud" # string; enum=["observe.cloud", "observe.providerPrivateIPs", "propose.dynamicConfig", "propose.providerAction", "execute.providerAction"]
    context: # 任意; object
      resources: # 任意; array
        - # object
          apiVersion: "<string>" # 必須; string
          kind: "<string>" # 必須; string
          name: "<string>" # 必須; string
    env: # 任意; object
      "<key>": "<string>" # 任意; string
    executable: "<string>" # 必須; string
    timeout: "<string>" # 任意; string
    triggers: # 任意; array
      - # object
        every: "<string>" # 任意; string
        topic: "<string>" # 任意; string
        type: "interval" # 必須; string; enum=["interval", "event"]
```

## 項目の意味と既定値

| YAML | 項目の意味と既定値 |
| --- | --- |
| `spec.executable` | 外部プログラムの絶対パス。必須で既定値はありません。 |
| `spec.timeout` | 1回の実行時間の上限。省略時10s。指定する場合は30sなど、正のGo duration形式です。 |
| `spec.env` | 環境変数名をキーにした追加変数。省略時は空です。runner自身のPATH、なければ組込PATHを用意してから上書きします。それ以外の親環境変数は継承しません。空のキーと=を含むキーは無視します。 |
| `spec.capabilities` | 能力の宣言。省略時は空です。observe.cloud、observe.providerPrivateIPs、propose.dynamicConfig、propose.providerAction、execute.providerActionを受理します。インベントリ取得にはobserve.providerPrivateIPs、事業者操作の実行にはexecute.providerActionが必要です。この一覧自体は実行を開始しません。 |
| `spec.context` | 標準入力に渡す設定リソースの許可一覧。省略時は空で、設定を渡しません。 |
| `spec.context.resources` | 許可するリソース識別子の一覧。省略時は空です。ワイルドカード展開は行いません。 |
| `spec.context.resources[].apiVersion` | 参照する設定リソースのAPIグループとバージョン。各要素で必須です。 |
| `spec.context.resources[].kind` | 参照する設定リソースの種類。各要素で必須です。 |
| `spec.context.resources[].name` | 参照する設定リソースのmetadata.name。各要素で必須です。 |
| `spec.triggers` | トリガー宣言の一覧。省略時は空です。現行runtimeでは検証・表示だけで、動作条件は上記の注意事項を参照してください。 |
| `spec.triggers[].type` | トリガーの区分。intervalかeventの指定が必須で、既定値はありません。 |
| `spec.triggers[].every` | intervalでは必須の正のGo duration。eventではこの項目による周期実行はありません。既定値はありません。 |
| `spec.triggers[].topic` | イベントトピックの宣言。任意で省略時は空です。eventでも検証上は必須になりません。リソース名ではありません。 |

実装: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_plugin.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_plugin.go) · [pkg/plugin/runner.go](https://github.com/imksoo/routerd/blob/main/pkg/plugin/runner.go) · [pkg/plugin/context.go](https://github.com/imksoo/routerd/blob/main/pkg/plugin/context.go) · [pkg/providerinventory/executor.go](https://github.com/imksoo/routerd/blob/main/pkg/providerinventory/executor.go) · [pkg/provideraction/executor.go](https://github.com/imksoo/routerd/blob/main/pkg/provideraction/executor.go)

[設定例](https://github.com/imksoo/routerd/blob/main/examples/cloudedge-mobility-demo/aws.yaml)
