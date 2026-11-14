# FirewallRule

[リソース別の設定](../resources.md#firewallrule) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: firewall.routerd.net/v1alpha1` · `metadata.name`

## YAML定義

全フィールドを示す構造参照です。そのまま反映する設定例ではありません。プレースホルダーを実値に置換し、不要な項目を省き、両立する選択肢だけを使います。表示値は型を示す仮値であり、実効的な既定値と指定条件は下の項目説明に記載します。必須の表示はJSON Schemaに基づく親オブジェクト内での指定要否であり、意味上の検証では追加の項目や参照が必要になる場合があります。

```yaml
- apiVersion: firewall.routerd.net/v1alpha1
  kind: FirewallRule
  metadata:
    name: "<name>"
  spec:
    action: "accept" # 必須; string; enum=["accept", "drop", "reject"]
    connLimit: # 任意; object
      log: true # 任意; boolean
      maxPerSource: 0 # 任意; integer; minimum=0
    destinationCIDRs: # 任意; array
      - "<string>" # string
    destinationPorts: # 任意; array
      - "<string>" # string
    destinationSetRefs: # 任意; array
      - "<string>" # string
    excludeDestinationSetRefs: # 任意; array
      - "<string>" # string
    fromZone: "<string>" # 必須; string
    icmpType: "<string>" # 任意; string
    icmpv6Type: "<string>" # 任意; string
    log: true # 任意; boolean
    port: 0 # 任意; integer; minimum=0; maximum=65535
    protocol: "" # 任意; string; enum=["", "tcp", "udp", "icmp", "icmpv6", "ipv6-icmp", "ipip"]
    rateLimit: # 任意; object
      burst: 0 # 任意; integer; minimum=0
      log: true # 任意; boolean
      packetsPerSecond: 0 # 任意; integer; minimum=0
      per: "" # 任意; string; enum=["", "second", "minute"]
      rate: 0 # 任意; integer; minimum=0
      unit: "" # 任意; string; enum=["", "packet", "byte", "kilobyte", "megabyte"]
    sourcePorts: # 任意; array
      - "<string>" # string
    srcCIDRs: # 任意; array
      - "<string>" # string
    toZone: "<string>" # 必須; string
```

[設定例](https://github.com/imksoo/routerd/blob/main/examples/firewall-rate-limit.yaml)
