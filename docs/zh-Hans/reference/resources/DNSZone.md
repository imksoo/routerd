# DNSZone

[各资源的配置](../resources.md#dnszone) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

## YAML定义

此处列出所有字段的结构，不是可直接应用的配置示例。请替换占位值、省略不使用的选项，并选择兼容的组合。显示值仅表示类型；有效默认值及条件列于下方字段说明。必填标记来自JSON Schema，表示在所属对象内必须指定；语义验证可能要求其他字段或引用。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DNSZone
  metadata:
    name: "<name>"
  spec:
    dhcpDerived: # 选填; object
      ddns: true # 选填; boolean
      hostnameSuffix: "<string>" # 选填; string
      leaseFile: "<string>" # 选填; string
      sources: # 选填; array
        - "<string>" # string
      ttl: 0 # 选填; integer; minimum=0
    dnssec: # 选填; object
      enabled: true # 选填; boolean
      mode: "" # 选填; string; enum=["", "validate", "sign"]
    records: # 选填; array
      - # object
        hostname: "<string>" # 必填; string
        ipv4: "<string>" # 选填; string
        ipv4From: # 选填; object
          field: "<string>" # 选填; string
          optional: true # 选填; boolean
          resource: "<string>" # 必填; string
        ipv6: "<string>" # 选填; string
        ipv6From: # 选填; object
          field: "<string>" # 选填; string
          optional: true # 选填; boolean
          resource: "<string>" # 必填; string
        ttl: 0 # 选填; integer; minimum=0
    reverseZones: # 选填; array
      - # object
        name: "<string>" # 必填; string
    ttl: 0 # 选填; integer; minimum=0
    zone: "<string>" # 必填; string
```

[配置示例](https://github.com/imksoo/routerd/blob/main/examples/example-dslite-home.yaml)
