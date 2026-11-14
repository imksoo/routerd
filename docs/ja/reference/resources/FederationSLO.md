# FederationSLO

[リソース別の設定](../resources.md#federationslo) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: federation.routerd.net/v1alpha1` · `metadata.name`

## YAML定義

全フィールドを示す構造参照です。そのまま反映する設定例ではありません。プレースホルダーを実値に置換し、不要な項目を省き、両立する選択肢だけを使います。表示値は型を示す仮値であり、実効的な既定値と指定条件は下の項目説明に記載します。必須の表示はJSON Schemaに基づく親オブジェクト内での指定要否であり、意味上の検証では追加の項目や参照が必要になる場合があります。

```yaml
- apiVersion: federation.routerd.net/v1alpha1
  kind: FederationSLO
  metadata:
    name: "<name>"
  spec:
    delivery: # 任意; object
      expiresSoonSeconds: 0 # 任意; integer; minimum=0
      lagFailSeconds: 0 # 任意; integer; minimum=0
      lagWarnSeconds: 0 # 任意; integer; minimum=0
    groupRef: "<string>" # 必須; string
    subscription: # 任意; object
      maxFailedRuns: 0 # 任意; integer; minimum=0
      maxPendingRuns: 0 # 任意; integer; minimum=0
```
