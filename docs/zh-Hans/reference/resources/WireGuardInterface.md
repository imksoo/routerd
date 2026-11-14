# WireGuardInterface

[各资源的配置](../resources.md#wireguardinterface) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

## YAML定义

此处列出所有字段的结构，不是可直接应用的配置示例。请替换占位值、省略不使用的选项，并选择兼容的组合。显示值仅表示类型；有效默认值及条件列于下方字段说明。必填标记来自JSON Schema，表示在所属对象内必须指定；语义验证可能要求其他字段或引用。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: WireGuardInterface
  metadata:
    name: "<name>"
  spec:
    ifname: "<string>" # 选填; string
    listenPort: 1 # 选填; integer; minimum=1; maximum=65535
    mtu: 576 # 选填; integer; minimum=576; maximum=9216
    peersFrom: # 选填; array
      - # object
        optional: true # 选填; boolean
        resource: "<string>" # 必填; string
    privateKey: "<string>" # 选填; string
    privateKeyFile: "<string>" # 选填; string
    selfNodeRef: "<string>" # 选填; string
```

[配置示例](https://github.com/imksoo/routerd/blob/main/examples/cloudedge-aws-rr-dynamic-leaf.yaml)
