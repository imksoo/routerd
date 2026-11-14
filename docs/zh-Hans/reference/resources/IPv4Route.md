# IPv4Route

[各资源的配置](../resources.md#ipv4route) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

## YAML定义

此处列出所有字段的结构，不是可直接应用的配置示例。请替换占位值、省略不使用的选项，并选择兼容的组合。显示值仅表示类型；有效默认值及条件列于下方字段说明。必填标记来自JSON Schema，表示在所属对象内必须指定；语义验证可能要求其他字段或引用。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: IPv4Route
  metadata:
    name: "<name>"
  spec:
    dependsOn: # 选填; array
      - # object
        equals: "<string>" # 选填; string
        field: "<string>" # 选填; string
        notEmpty: true # 选填; boolean
        optional: true # 选填; boolean
        phase: "<string>" # 选填; string
        resource: "<string>" # 必填; string
    destination: "<string>" # 必填; string
    device: "<string>" # 选填; string
    deviceFrom: # 选填; object
      field: "<string>" # 选填; string
      optional: true # 选填; boolean
      resource: "<string>" # 必填; string
    gateway: "<string>" # 选填; string
    gatewayFrom: # 选填; object
      field: "<string>" # 选填; string
      optional: true # 选填; boolean
      resource: "<string>" # 必填; string
    metric: 0 # 选填; integer; minimum=0
    preferredSource: "<string>" # 选填; string
    type: "unicast" # 选填; string; enum=["unicast", "blackhole"]
```

[配置示例](https://github.com/imksoo/routerd/blob/main/examples/lan-advertise-with-community.yaml)
