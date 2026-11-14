# FirewallZone

[各资源的配置](../resources.md#firewallzone) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: firewall.routerd.net/v1alpha1` · `metadata.name`

## YAML定义

此处列出所有字段的结构，不是可直接应用的配置示例。请替换占位值、省略不使用的选项，并选择兼容的组合。显示值仅表示类型；有效默认值及条件列于下方字段说明。必填标记来自JSON Schema，表示在所属对象内必须指定；语义验证可能要求其他字段或引用。

```yaml
- apiVersion: firewall.routerd.net/v1alpha1
  kind: FirewallZone
  metadata:
    name: "<name>"
  spec:
    interfaces: # 必填; array
      - "<string>" # string
    role: "untrust" # 必填; string; enum=["untrust", "trust", "mgmt"]
```

[配置示例](https://github.com/imksoo/routerd/blob/main/examples/example-basic-ipv4-nat.yaml)
