# FirewallRule

[各資源的設定](../resources.md#firewallrule) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: firewall.routerd.net/v1alpha1` · `metadata.name`

## YAML定義

此處列出所有欄位的結構，不是可直接套用的設定範例。請替換佔位值、省略不使用的選項，並選擇相容的組合。顯示值只表示型別；有效預設值及條件列於下方欄位說明。必填標記來自JSON Schema，表示在所屬物件內必須指定；語意驗證可能要求其他欄位或參照。

```yaml
- apiVersion: firewall.routerd.net/v1alpha1
  kind: FirewallRule
  metadata:
    name: "<name>"
  spec:
    action: "accept" # 必填; string; enum=["accept", "drop", "reject"]
    connLimit: # 選填; object
      log: true # 選填; boolean
      maxPerSource: 0 # 選填; integer; minimum=0
    destinationCIDRs: # 選填; array
      - "<string>" # string
    destinationPorts: # 選填; array
      - "<string>" # string
    destinationSetRefs: # 選填; array
      - "<string>" # string
    excludeDestinationSetRefs: # 選填; array
      - "<string>" # string
    fromZone: "<string>" # 必填; string
    icmpType: "<string>" # 選填; string
    icmpv6Type: "<string>" # 選填; string
    log: true # 選填; boolean
    port: 0 # 選填; integer; minimum=0; maximum=65535
    protocol: "" # 選填; string; enum=["", "tcp", "udp", "icmp", "icmpv6", "ipv6-icmp", "ipip"]
    rateLimit: # 選填; object
      burst: 0 # 選填; integer; minimum=0
      log: true # 選填; boolean
      packetsPerSecond: 0 # 選填; integer; minimum=0
      per: "" # 選填; string; enum=["", "second", "minute"]
      rate: 0 # 選填; integer; minimum=0
      unit: "" # 選填; string; enum=["", "packet", "byte", "kilobyte", "megabyte"]
    sourcePorts: # 選填; array
      - "<string>" # string
    srcCIDRs: # 選填; array
      - "<string>" # string
    toZone: "<string>" # 必填; string
```

[設定範例](https://github.com/imksoo/routerd/blob/main/examples/firewall-rate-limit.yaml)
