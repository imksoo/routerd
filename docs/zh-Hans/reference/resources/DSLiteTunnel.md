# DSLiteTunnel

[各资源的配置](../resources.md#dslitetunnel) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

## YAML定义

此处列出所有字段的结构，不是可直接应用的配置示例。请替换占位值、省略不使用的选项，并选择兼容的组合。显示值仅表示类型；有效默认值及条件列于下方字段说明。必填标记来自JSON Schema，表示在所属对象内必须指定；语义验证可能要求其他字段或引用。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DSLiteTunnel
  metadata:
    name: "<name>"
  spec:
    aftrAddressOrdinal: 1 # 选填; integer; minimum=1
    aftrAddressSelection: "ordinal" # 选填; string; enum=["ordinal", "ordinalModulo"]
    aftrDNSServers: # 选填; array
      - "<string>" # string
    aftrFQDN: "<string>" # 选填; string
    aftrFrom: # 选填; object
      field: "<string>" # 选填; string
      optional: true # 选填; boolean
      resource: "<string>" # 必填; string
    aftrIPv6: "<string>" # 选填; string
    defaultRoute: true # 选填; boolean
    dependsOn: # 选填; array
      - # object
        equals: "<string>" # 选填; string
        field: "<string>" # 选填; string
        notEmpty: true # 选填; boolean
        optional: true # 选填; boolean
        phase: "<string>" # 选填; string
        resource: "<string>" # 必填; string
    enabled: true # 选填; boolean
    encapsulationLimit: "<string>" # 选填; string
    interface: "<string>" # 必填; string
    localAddress: "<string>" # 选填; string
    localAddressFrom: # 选填; object
      field: "<string>" # 选填; string
      optional: true # 选填; boolean
      resource: "<string>" # 必填; string
    localAddressSource: "interface" # 选填; string; enum=["interface", "static", "delegatedAddress"]
    localAddressSuffix: "<string>" # 选填; string
    localDelegatedAddress: "<string>" # 选填; string
    mtu: 1280 # 选填; integer; minimum=1280; maximum=65535
    remoteAddress: "<string>" # 选填; string
    routeMetric: 0 # 选填; integer; minimum=0
    tunnelName: "<string>" # 选填; string
    when: # 选填; object
      state: # 选填; object
        "<key>": # 选填; object
          contains: "<string>" # 选填; string
          equals: "<string>" # 选填; string
          exists: true # 选填; boolean
          for: "<string>" # 选填; string
          in: # 选填; array
            - "<string>" # string
          maxAge: "<string>" # 选填; string
          status: "set" # 选填; string; enum=["set", "unset", "unknown"]
```

[配置示例](https://github.com/imksoo/routerd/blob/main/examples/example-dslite-home.yaml)
