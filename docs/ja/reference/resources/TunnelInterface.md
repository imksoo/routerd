# TunnelInterface

[リソース別の設定](../resources.md#tunnelinterface) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: hybrid.routerd.net/v1alpha1` · `metadata.name`

## YAML定義

全フィールドを示す構造参照です。そのまま反映する設定例ではありません。プレースホルダーを実値に置換し、不要な項目を省き、両立する選択肢だけを使います。表示値は型を示す仮値であり、実効的な既定値と指定条件は下の項目説明に記載します。必須の表示はJSON Schemaに基づく親オブジェクト内での指定要否であり、意味上の検証では追加の項目や参照が必要になる場合があります。

```yaml
- apiVersion: hybrid.routerd.net/v1alpha1
  kind: TunnelInterface
  metadata:
    name: "<name>"
  spec:
    address: "<string>" # 任意; string
    encapDport: 1 # 任意; integer; minimum=1; maximum=65535
    encapSport: 1 # 任意; integer; minimum=1; maximum=65535
    key: 0 # 任意; integer; minimum=0; maximum=4294967295
    local: "<string>" # 任意; string
    localFrom: # 任意; object
      field: "<string>" # 任意; string
      optional: true # 任意; boolean
      resource: "<string>" # 必須; string
    mode: "ipip" # 必須; string; enum=["ipip", "gre", "fou", "gue"]
    mtu: 576 # 任意; integer; minimum=576; maximum=9216
    pathMTU: # 任意; object
      forceFragmentIPv4: true # 任意; boolean
    peerAddress: "<string>" # 任意; string
    remote: "<string>" # 任意; string
    remoteFrom: # 任意; object
      field: "<string>" # 任意; string
      optional: true # 任意; boolean
      resource: "<string>" # 必須; string
    trustedUnderlay: true # 必須; boolean
    ttl: 1 # 任意; integer; minimum=1; maximum=255
    underlayInterface: "<string>" # 任意; string
```

[設定例](https://github.com/imksoo/routerd/blob/main/examples/hybrid-l3-fou.yaml)
