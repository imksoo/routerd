# DSLiteTunnel

[リソース別の設定](../resources.md#dslitetunnel) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

## YAML定義

全フィールドを示す構造参照です。そのまま反映する設定例ではありません。プレースホルダーを実値に置換し、不要な項目を省き、両立する選択肢だけを使います。表示値は型を示す仮値であり、実効的な既定値と指定条件は下の項目説明に記載します。必須の表示はJSON Schemaに基づく親オブジェクト内での指定要否であり、意味上の検証では追加の項目や参照が必要になる場合があります。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DSLiteTunnel
  metadata:
    name: "<name>"
  spec:
    aftrAddressOrdinal: 1 # 任意; integer; minimum=1
    aftrAddressSelection: "ordinal" # 任意; string; enum=["ordinal", "ordinalModulo"]
    aftrDNSServers: # 任意; array
      - "<string>" # string
    aftrFQDN: "<string>" # 任意; string
    aftrFrom: # 任意; object
      field: "<string>" # 任意; string
      optional: true # 任意; boolean
      resource: "<string>" # 必須; string
    aftrIPv6: "<string>" # 任意; string
    defaultRoute: true # 任意; boolean
    dependsOn: # 任意; array
      - # object
        equals: "<string>" # 任意; string
        field: "<string>" # 任意; string
        notEmpty: true # 任意; boolean
        optional: true # 任意; boolean
        phase: "<string>" # 任意; string
        resource: "<string>" # 必須; string
    enabled: true # 任意; boolean
    encapsulationLimit: "<string>" # 任意; string
    interface: "<string>" # 必須; string
    localAddress: "<string>" # 任意; string
    localAddressFrom: # 任意; object
      field: "<string>" # 任意; string
      optional: true # 任意; boolean
      resource: "<string>" # 必須; string
    localAddressSource: "interface" # 任意; string; enum=["interface", "static", "delegatedAddress"]
    localAddressSuffix: "<string>" # 任意; string
    localDelegatedAddress: "<string>" # 任意; string
    mtu: 1280 # 任意; integer; minimum=1280; maximum=65535
    remoteAddress: "<string>" # 任意; string
    routeMetric: 0 # 任意; integer; minimum=0
    tunnelName: "<string>" # 任意; string
    when: # 任意; object
      state: # 任意; object
        "<key>": # 任意; object
          contains: "<string>" # 任意; string
          equals: "<string>" # 任意; string
          exists: true # 任意; boolean
          for: "<string>" # 任意; string
          in: # 任意; array
            - "<string>" # string
          maxAge: "<string>" # 任意; string
          status: "set" # 任意; string; enum=["set", "unset", "unknown"]
```

[設定例](https://github.com/imksoo/routerd/blob/main/examples/example-dslite-home.yaml)
