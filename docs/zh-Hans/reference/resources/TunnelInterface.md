# TunnelInterface

[各资源的配置](../resources.md#tunnelinterface) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: hybrid.routerd.net/v1alpha1` · `metadata.name`

## YAML定义

此处列出所有字段的结构，不是可直接应用的配置示例。请替换占位值、省略不使用的选项，并选择兼容的组合。显示值仅表示类型；有效默认值及条件列于下方字段说明。必填标记来自JSON Schema，表示在所属对象内必须指定；语义验证可能要求其他字段或引用。

```yaml
- apiVersion: hybrid.routerd.net/v1alpha1
  kind: TunnelInterface
  metadata:
    name: "<name>"
  spec:
    address: "<string>" # 选填; string
    encapDport: 1 # 选填; integer; minimum=1; maximum=65535
    encapSport: 1 # 选填; integer; minimum=1; maximum=65535
    key: 0 # 选填; integer; minimum=0; maximum=4294967295
    local: "<string>" # 选填; string
    localFrom: # 选填; object
      field: "<string>" # 选填; string
      optional: true # 选填; boolean
      resource: "<string>" # 必填; string
    mode: "ipip" # 必填; string; enum=["ipip", "gre", "fou", "gue"]
    mtu: 576 # 选填; integer; minimum=576; maximum=9216
    pathMTU: # 选填; object
      forceFragmentIPv4: true # 选填; boolean
    peerAddress: "<string>" # 选填; string
    remote: "<string>" # 选填; string
    remoteFrom: # 选填; object
      field: "<string>" # 选填; string
      optional: true # 选填; boolean
      resource: "<string>" # 必填; string
    trustedUnderlay: true # 必填; boolean
    ttl: 1 # 选填; integer; minimum=1; maximum=255
    underlayInterface: "<string>" # 选填; string
```

[配置示例](https://github.com/imksoo/routerd/blob/main/examples/hybrid-l3-fou.yaml)
