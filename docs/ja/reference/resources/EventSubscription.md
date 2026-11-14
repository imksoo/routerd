# EventSubscription

[リソース別の設定](../resources.md#eventsubscription) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: federation.routerd.net/v1alpha1` · `metadata.name`

## YAML定義

全フィールドを示す構造参照です。そのまま反映する設定例ではありません。プレースホルダーを実値に置換し、不要な項目を省き、両立する選択肢だけを使います。表示値は型を示す仮値であり、実効的な既定値と指定条件は下の項目説明に記載します。必須の表示はJSON Schemaに基づく親オブジェクト内での指定要否であり、意味上の検証では追加の項目や参照が必要になる場合があります。

```yaml
- apiVersion: federation.routerd.net/v1alpha1
  kind: EventSubscription
  metadata:
    name: "<name>"
  spec:
    groupRef: "<string>" # 必須; string
    match: # 必須; object
      payload: # 任意; object
        "<key>": "<string>" # 任意; string
      sourceNodes: # 任意; array
        - "<string>" # string
      subjectPrefixes: # 任意; array
        - "<string>" # string
      types: # 必須; array
        - "<string>" # string
    trigger: # 必須; object
      batchWindow: "<string>" # 任意; string
      debounce: "<string>" # 任意; string
      pluginRef: "<string>" # 必須; string
```
