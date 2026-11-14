# SAMNodeSet

[リソース別の設定](../resources.md#samnodeset) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: mobility.routerd.net/v1alpha1` · `metadata.name`

## YAML定義

全フィールドを示す構造参照です。そのまま反映する設定例ではありません。プレースホルダーを実値に置換し、不要な項目を省き、両立する選択肢だけを使います。表示値は型を示す仮値であり、実効的な既定値と指定条件は下の項目説明に記載します。必須の表示はJSON Schemaに基づく親オブジェクト内での指定要否であり、意味上の検証では追加の項目や参照が必要になる場合があります。

```yaml
- apiVersion: mobility.routerd.net/v1alpha1
  kind: SAMNodeSet
  metadata:
    name: "<name>"
  spec:
    nodes: # 必須; array
      - # object
        eventEndpoint: "<string>" # 任意; string
        macAddresses: # 任意; array
          - "<string>" # string; pattern="^([0-9A-Fa-f]{2}:){5}[0-9A-Fa-f]{2}$"
        maintenance: # 任意; object
          drain: true # 任意; boolean
        maxSecondaryIPs: 0 # 任意; integer; minimum=0
        nodeRef: "<string>" # 必須; string
        placement: # 任意; object
          group: "<string>" # 任意; string
          priority: 0 # 任意; integer; minimum=0; maximum=1000000
        role: "" # 任意; string; enum=["", "onprem", "cloud"]
        routeReflector: true # 任意; boolean
        samEndpoint: "<string>" # 任意; string
        samEndpointFrom: # 任意; object
          field: "<string>" # 任意; string
          optional: true # 任意; boolean
          resource: "<string>" # 必須; string
        site: "<string>" # 任意; string
        wireGuard: # 任意; object
          allowedIPs: # 任意; array
            - "<string>" # string
          endpoint: "<string>" # 任意; string
          persistentKeepalive: 0 # 任意; integer; minimum=0; maximum=65535
          publicKey: "<string>" # 任意; string
          transportEndpoint: "<string>" # 任意; string
```

[設定例](https://github.com/imksoo/routerd/blob/main/examples/cloudedge-dynamic-rr-a-hub.yaml)
