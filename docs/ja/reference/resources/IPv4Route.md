# IPv4Route

[リソース別の設定](../resources.md#ipv4route) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

## YAML定義

全フィールドを示す構造参照です。そのまま反映する設定例ではありません。プレースホルダーを実値に置換し、不要な項目を省き、両立する選択肢だけを使います。表示値は型を示す仮値であり、実効的な既定値と指定条件は下の項目説明に記載します。必須の表示はJSON Schemaに基づく親オブジェクト内での指定要否であり、意味上の検証では追加の項目や参照が必要になる場合があります。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: IPv4Route
  metadata:
    name: "<name>"
  spec:
    dependsOn: # 任意; array
      - # object
        equals: "<string>" # 任意; string
        field: "<string>" # 任意; string
        notEmpty: true # 任意; boolean
        optional: true # 任意; boolean
        phase: "<string>" # 任意; string
        resource: "<string>" # 必須; string
    destination: "<string>" # 必須; string
    device: "<string>" # 任意; string
    deviceFrom: # 任意; object
      field: "<string>" # 任意; string
      optional: true # 任意; boolean
      resource: "<string>" # 必須; string
    gateway: "<string>" # 任意; string
    gatewayFrom: # 任意; object
      field: "<string>" # 任意; string
      optional: true # 任意; boolean
      resource: "<string>" # 必須; string
    metric: 0 # 任意; integer; minimum=0
    preferredSource: "<string>" # 任意; string
    type: "unicast" # 任意; string; enum=["unicast", "blackhole"]
```

[設定例](https://github.com/imksoo/routerd/blob/main/examples/lan-advertise-with-community.yaml)
