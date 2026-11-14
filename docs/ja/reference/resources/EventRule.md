# EventRule

[リソース別の設定](../resources.md#eventrule) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

## YAML定義

全フィールドを示す構造参照です。そのまま反映する設定例ではありません。プレースホルダーを実値に置換し、不要な項目を省き、両立する選択肢だけを使います。表示値は型を示す仮値であり、実効的な既定値と指定条件は下の項目説明に記載します。必須の表示はJSON Schemaに基づく親オブジェクト内での指定要否であり、意味上の検証では追加の項目や参照が必要になる場合があります。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: EventRule
  metadata:
    name: "<name>"
  spec:
    emit: # 必須; object
      attributes: # 任意; object
        "<key>": "<string>" # 任意; string
      topic: "<string>" # 必須; string
    pattern: # 必須; object
      allow_missing_correlation: true # 任意; boolean
      correlate_by: "<string>" # 任意; string
      duration: "<string>" # 任意; string
      expected: "<string>" # 任意; string
      interval: "<string>" # 任意; string
      operator: "all_of" # 必須; string; enum=["all_of", "any_of", "sequence", "window", "absence", "throttle", "debounce", "count"]
      quiet: "<string>" # 任意; string
      rate: 0 # 任意; integer; minimum=0
      strict: true # 任意; boolean
      threshold: 0 # 任意; integer; minimum=0
      topic: "<string>" # 任意; string
      topics: # 任意; array
        - "<string>" # string
      trigger: "<string>" # 任意; string
      window: "<string>" # 任意; string
```
