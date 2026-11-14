# Telemetry

[リソース別の設定](../resources.md#telemetry) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: observability.routerd.net/v1alpha1` · `metadata.name`

## YAML定義

全フィールドを示す構造参照です。そのまま反映する設定例ではありません。プレースホルダーを実値に置換し、不要な項目を省き、両立する選択肢だけを使います。表示値は型を示す仮値であり、実効的な既定値と指定条件は下の項目説明に記載します。必須の表示はJSON Schemaに基づく親オブジェクト内での指定要否であり、意味上の検証では追加の項目や参照が必要になる場合があります。

```yaml
- apiVersion: observability.routerd.net/v1alpha1
  kind: Telemetry
  metadata:
    name: "<name>"
  spec:
    attributes: # 任意; object
      "<key>": "<string>" # 任意; string
    otlp: # 必須; object
      endpoint: "<string>" # 必須; string
      insecure: true # 任意; boolean
    serviceNamespace: "<string>" # 任意; string
    signals: # 任意; array
      - "logs" # string; enum=["logs", "metrics", "traces"]
```

[設定例](https://github.com/imksoo/routerd/blob/main/examples/telemetry-export.yaml)
