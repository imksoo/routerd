# NAT44Rule

[各资源的配置](../resources.md#nat44rule) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

## YAML定义

此处列出所有字段的结构，不是可直接应用的配置示例。请替换占位值、省略不使用的选项，并选择兼容的组合。显示值仅表示类型；有效默认值及条件列于下方字段说明。必填标记来自JSON Schema，表示在所属对象内必须指定；语义验证可能要求其他字段或引用。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: NAT44Rule
  metadata:
    name: "<name>"
  spec:
    destinationCIDRs: # 选填; array
      - "<string>" # string
    destinationSetRefs: # 选填; array
      - "<string>" # string
    egressInterface: "<string>" # 选填; string
    egressPolicyRef: "<string>" # 选填; string
    excludeDestinationCIDRs: # 选填; array
      - "<string>" # string
    excludeDestinationSetRefs: # 选填; array
      - "<string>" # string
    outboundInterface: "<string>" # 选填; string
    snatAddress: "<string>" # 选填; string
    snatAddressFrom: # 选填; object
      field: "<string>" # 选填; string
      optional: true # 选填; boolean
      resource: "<string>" # 必填; string
    sourceCIDRs: # 选填; array
      - "<string>" # string
    sourceRanges: # 选填; array
      - "<string>" # string
    translation: # 选填; object
      address: "<string>" # 选填; string
      addresses: # 选填; array
        - "<string>" # string
      portMapping: # 选填; object
        end: 1 # 选填; integer; minimum=1; maximum=65535
        start: 1 # 选填; integer; minimum=1; maximum=65535
        type: "auto" # 选填; string; enum=["auto", "preserve", "range"]
      type: "interfaceAddress" # 必填; string; enum=["interfaceAddress", "address", "pool"]
    type: "masquerade" # 选填; string; enum=["masquerade", "snat"]
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

[配置示例](https://github.com/imksoo/routerd/blob/main/examples/dslite-lan-range-snat.yaml)
