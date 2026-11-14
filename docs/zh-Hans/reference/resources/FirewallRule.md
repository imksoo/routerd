# FirewallRule

[各资源的配置](../resources.md#firewallrule) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: firewall.routerd.net/v1alpha1` · `metadata.name`

## YAML定义

此处列出所有字段的结构，不是可直接应用的配置示例。请替换占位值、省略不使用的选项，并选择兼容的组合。显示值仅表示类型；有效默认值及条件列于下方字段说明。必填标记来自JSON Schema，表示在所属对象内必须指定；语义验证可能要求其他字段或引用。

```yaml
- apiVersion: firewall.routerd.net/v1alpha1
  kind: FirewallRule
  metadata:
    name: "<name>"
  spec:
    action: "accept" # 必填; string; enum=["accept", "drop", "reject"]
    connLimit: # 选填; object
      log: true # 选填; boolean
      maxPerSource: 0 # 选填; integer; minimum=0
    destinationCIDRs: # 选填; array
      - "<string>" # string
    destinationPorts: # 选填; array
      - "<string>" # string
    destinationSetRefs: # 选填; array
      - "<string>" # string
    excludeDestinationSetRefs: # 选填; array
      - "<string>" # string
    fromZone: "<string>" # 必填; string
    icmpType: "<string>" # 选填; string
    icmpv6Type: "<string>" # 选填; string
    log: true # 选填; boolean
    port: 0 # 选填; integer; minimum=0; maximum=65535
    protocol: "" # 选填; string; enum=["", "tcp", "udp", "icmp", "icmpv6", "ipv6-icmp", "ipip"]
    rateLimit: # 选填; object
      burst: 0 # 选填; integer; minimum=0
      log: true # 选填; boolean
      packetsPerSecond: 0 # 选填; integer; minimum=0
      per: "" # 选填; string; enum=["", "second", "minute"]
      rate: 0 # 选填; integer; minimum=0
      unit: "" # 选填; string; enum=["", "packet", "byte", "kilobyte", "megabyte"]
    sourcePorts: # 选填; array
      - "<string>" # string
    srcCIDRs: # 选填; array
      - "<string>" # string
    toZone: "<string>" # 必填; string
```

[配置示例](https://github.com/imksoo/routerd/blob/main/examples/firewall-rate-limit.yaml)
