# DNSZone

[リソース別の設定](../resources.md#dnszone) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

## YAML定義

全フィールドを示す構造参照です。そのまま反映する設定例ではありません。プレースホルダーを実値に置換し、不要な項目を省き、両立する選択肢だけを使います。表示値は型を示す仮値であり、実効的な既定値と指定条件は下の項目説明に記載します。必須の表示はJSON Schemaに基づく親オブジェクト内での指定要否であり、意味上の検証では追加の項目や参照が必要になる場合があります。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DNSZone
  metadata:
    name: "<name>"
  spec:
    dhcpDerived: # 任意; object
      ddns: true # 任意; boolean
      hostnameSuffix: "<string>" # 任意; string
      leaseFile: "<string>" # 任意; string
      sources: # 任意; array
        - "<string>" # string
      ttl: 0 # 任意; integer; minimum=0
    dnssec: # 任意; object
      enabled: true # 任意; boolean
      mode: "" # 任意; string; enum=["", "validate", "sign"]
    records: # 任意; array
      - # object
        hostname: "<string>" # 必須; string
        ipv4: "<string>" # 任意; string
        ipv4From: # 任意; object
          field: "<string>" # 任意; string
          optional: true # 任意; boolean
          resource: "<string>" # 必須; string
        ipv6: "<string>" # 任意; string
        ipv6From: # 任意; object
          field: "<string>" # 任意; string
          optional: true # 任意; boolean
          resource: "<string>" # 必須; string
        ttl: 0 # 任意; integer; minimum=0
    reverseZones: # 任意; array
      - # object
        name: "<string>" # 必須; string
    ttl: 0 # 任意; integer; minimum=0
    zone: "<string>" # 必須; string
```

[設定例](https://github.com/imksoo/routerd/blob/main/examples/example-dslite-home.yaml)
